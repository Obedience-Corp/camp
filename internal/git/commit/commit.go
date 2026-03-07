package commit

import (
	"context"
	"errors"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/git"
)

// Result contains the outcome of a commit attempt.
type Result struct {
	Committed bool   // True if commit succeeded
	Message   string // User-facing message
}

// Options configures common commit parameters.
type Options struct {
	CampaignRoot  string   // Path to campaign root
	CampaignID    string   // Campaign ID (truncated to 8 chars)
	Files         []string // If set, stage only these paths instead of everything
	PreStaged     []string // Paths already staged (included in --only commit scope, not re-staged)
	SelectiveOnly bool     // When true, never fall back to CommitAll; no-op if Files is empty
}

// doCommit stages all changes and commits with standardized format.
// Commit failures are non-fatal and reported via Result.
//
// Commit message format:
//
//	[OBEY-CAMPAIGN-{id}] Action: Subject
//
//	Optional description body
func doCommit(ctx context.Context, opts Options, action, subject, description string) Result {
	if opts.CampaignRoot == "" || opts.CampaignID == "" {
		return Result{
			Committed: false,
			Message:   "", // Silent failure - campaign info not available
		}
	}

	commitMsg := fmt.Sprintf("%s %s: %s",
		git.FormatCampaignTag(opts.CampaignID),
		action,
		subject,
	)
	if description != "" {
		commitMsg += "\n\n" + description
	}

	if err := stageAndCommit(ctx, opts, commitMsg); err != nil {
		if errors.Is(err, git.ErrNoChanges) {
			return Result{
				Committed: false,
				Message:   "(no changes to commit)",
			}
		}
		return Result{
			Committed: false,
			Message:   fmt.Sprintf("Warning: git commit failed: %v", err),
		}
	}

	return Result{
		Committed: true,
		Message:   "Committed changes to git",
	}
}

// stageAndCommit stages files and commits. If opts.Files is set, only those
// paths are staged and committed (using --only to scope the commit).
// opts.PreStaged paths are included in the --only commit scope but are NOT
// re-staged (they were already staged externally, e.g. via git add -u).
// When SelectiveOnly is true and no paths exist, returns ErrNoChanges instead
// of falling back to CommitAll. Otherwise all changes are staged (legacy behavior).
func stageAndCommit(ctx context.Context, opts Options, message string) error {
	commitScope := append(append([]string{}, opts.Files...), opts.PreStaged...)
	if len(commitScope) > 0 {
		if len(opts.Files) > 0 {
			if err := git.StageFiles(ctx, opts.CampaignRoot, opts.Files...); err != nil {
				return err
			}
		}
		return git.Commit(ctx, opts.CampaignRoot, &git.CommitOptions{
			Message: message,
			Only:    commitScope,
		})
	}
	if opts.SelectiveOnly {
		return git.ErrNoChanges
	}
	return git.CommitAll(ctx, opts.CampaignRoot, message)
}
