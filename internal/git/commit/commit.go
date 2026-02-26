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
	CampaignRoot string // Path to campaign root
	CampaignID   string // Campaign ID (truncated to 8 chars)
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

	if err := git.CommitAll(ctx, opts.CampaignRoot, commitMsg); err != nil {
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
