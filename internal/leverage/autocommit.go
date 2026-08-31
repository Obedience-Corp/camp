package leverage

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// DataDir is the campaign-relative directory holding every file the leverage
// commands write: config.json, authors.json, cache/, and snapshots/.
// It is also the only pathspec an autocommit is ever allowed to stage.
const DataDir = ".campaign/leverage"

// CommitStatus is what an autocommit attempt actually did.
type CommitStatus int

const (
	// CommitSkipped means the attempt never reached git.
	CommitSkipped CommitStatus = iota
	// CommitUnchanged means git found nothing under DataDir to record.
	CommitUnchanged
	// CommitCreated means a commit landed.
	CommitCreated
)

// CommitResult describes an autocommit attempt. Reason is set for every
// non-committing outcome so the caller can report why rather than go quiet.
type CommitResult struct {
	Status CommitStatus
	Hash   string
	Reason string
}

// CommitRequest is everything an autocommit needs. Subject is the short verb
// phrase appended to the "leverage:" prefix; Report is the rendered plain-text
// body, which must not carry terminal escapes.
type CommitRequest struct {
	CampaignRoot string
	CampaignName string
	CampaignID   string
	Subject      string
	Report       string
}

// ScopedCommitter commits paths in repoPath without disturbing the real index.
// It exists so tests can drive Autocommit without a git repository.
type ScopedCommitter func(ctx context.Context, repoPath string, paths []string, opts *git.CommitOptions) error

// AutocommitEnabled reports whether leverage commands may commit their own
// data. An unset field means enabled: the setting exists to turn the behavior
// off, so a config written before it existed keeps the default.
func (c *LeverageConfig) AutocommitEnabled() bool {
	return c == nil || c.Autocommit == nil || *c.Autocommit
}

// CommitMessage renders the commit message for req: a tagged subject line and
// the report as the body.
func CommitMessage(req CommitRequest) string {
	subject := git.PrependContextTagsFull(
		req.CampaignName, req.CampaignID, "", "", "", "leverage: "+req.Subject)
	if req.Report == "" {
		return subject
	}
	return subject + "\n\n" + req.Report
}

// Autocommit commits the campaign's leverage data directory and nothing else.
//
// It never prompts and never blocks: every precondition it cannot satisfy is a
// skip carrying a Reason for the caller to report. Only a genuine git failure
// is returned as an error, and callers treat that as a warning rather than a
// command failure, because the data is already on disk either way.
//
// commitFn is optional; nil takes git.CommitScoped.
func Autocommit(ctx context.Context, req CommitRequest, commitFn ScopedCommitter) (CommitResult, error) {
	if err := ctx.Err(); err != nil {
		return CommitResult{}, err
	}
	if req.CampaignRoot == "" {
		return CommitResult{Reason: "no camp root"}, nil
	}
	if !git.IsRepo(req.CampaignRoot) {
		return CommitResult{Reason: "camp root is not a git repository"}, nil
	}
	if _, err := os.Stat(filepath.Join(req.CampaignRoot, filepath.FromSlash(DataDir))); err != nil {
		return CommitResult{Reason: DataDir + " does not exist"}, nil
	}

	if commitFn == nil {
		commitFn = git.CommitScoped
	}
	err := commitFn(ctx, req.CampaignRoot, []string{DataDir}, &git.CommitOptions{
		Message: CommitMessage(req),
	})
	switch {
	case errors.Is(err, git.ErrNoChanges):
		return CommitResult{Status: CommitUnchanged, Reason: "no leverage changes to commit"}, nil
	case err != nil:
		return CommitResult{}, camperrors.Wrap(err, "committing "+DataDir)
	}

	return CommitResult{Status: CommitCreated, Hash: git.HeadSHA(ctx, req.CampaignRoot)}, nil
}
