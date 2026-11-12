package approvalkit

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// CommitStatus is what a ledger commit attempt did.
type CommitStatus int

const (
	// CommitUnchanged: there was nothing to commit, or nothing to commit into.
	CommitUnchanged CommitStatus = iota
	// CommitCreated: a commit of the approvals ledger was made.
	CommitCreated
)

// CommitResult describes a ledger commit attempt. Reason is set for every
// outcome that made no commit.
type CommitResult struct {
	Status CommitStatus
	Hash   string
	Reason string
}

// commitSubject is the fixed subject of a ledger commit. The events themselves
// say what happened; the commit only has to say which files moved.
const commitSubject = "approvals: record approval requests and decisions"

// scopedCommit commits paths in repoPath without disturbing the real index. A
// variable so tests can drive Commit without a git repository.
var scopedCommit = git.CommitScoped

// CommitMessage renders the tagged commit message for a ledger commit in the
// camp at campaignRoot. A camp whose config cannot be read still gets a
// message, just without the camp tag.
func CommitMessage(ctx context.Context, campaignRoot string) string {
	name, id := "", ""
	if cfg, err := config.LoadCampaignConfig(ctx, campaignRoot); err == nil && cfg != nil {
		name, id = cfg.Name, cfg.ID
	}
	return git.PrependContextTagsFull(name, id, "", "", "", commitSubject)
}

// Commit commits the camp's approvals ledger directory and nothing else.
//
// A request that is never committed never reaches the user's other machines,
// which is the reason the ledger is committed at all. Recording an event does
// not commit it: one commit per request and per decision is noise in the camp's
// history, and a request only travels at the next push anyway. The ledger is
// committed in batches instead, by the camp's ordinary commits (it is a tracked
// path like any other) and by this function, which 'camp approvals' calls so
// nothing is left behind before the user looks. Only .campaign/approvals is
// staged, through a temporary index, so nothing the user has staged or left
// dirty is touched.
//
// It never prompts and never blocks on a missing precondition: a camp with no
// ledger, or one that is not a git repository, yields a result with a Reason,
// not an error. Only a genuine git failure is an error, and callers report
// that as a warning, because the events are on disk either way and the next
// call commits them.
func Commit(ctx context.Context, campaignRoot string) (CommitResult, error) {
	if err := ctx.Err(); err != nil {
		return CommitResult{}, err
	}
	if campaignRoot == "" {
		return CommitResult{Reason: "no camp root"}, nil
	}
	if _, err := os.Stat(filepath.Join(campaignRoot, filepath.FromSlash(Dir))); err != nil {
		return CommitResult{Reason: "no approvals have been recorded"}, nil
	}
	if !git.IsRepo(campaignRoot) {
		return CommitResult{Reason: "camp root is not a git repository"}, nil
	}
	err := scopedCommit(ctx, campaignRoot, []string{Dir}, &git.CommitOptions{
		Message: CommitMessage(ctx, campaignRoot),
	})
	switch {
	case errors.Is(err, git.ErrNoChanges):
		return CommitResult{Reason: "no approval events to commit"}, nil
	case err != nil:
		return CommitResult{}, camperrors.Wrap(err, "committing "+Dir)
	}
	return CommitResult{Status: CommitCreated, Hash: git.HeadSHA(ctx, campaignRoot)}, nil
}

// Uncommitted reports how many approvals ledger files have changes that are
// not committed. It is zero when the camp is not a git repository.
func Uncommitted(ctx context.Context, campaignRoot string) int {
	if campaignRoot == "" || !git.IsRepo(campaignRoot) {
		return 0
	}
	out, err := git.StatusPorcelain(ctx, campaignRoot, "--untracked-files=all", "--", Dir)
	if err != nil {
		return 0
	}
	return len(git.ParseStatusPorcelainZ(out))
}
