package git

import (
	"context"
	"errors"
	"fmt"
)

// ProjectAction represents the type of project operation performed.
type ProjectAction string

const (
	ProjectActionAdd    ProjectAction = "Add"
	ProjectActionNew    ProjectAction = "New"
	ProjectActionRemove ProjectAction = "Remove"
)

// ProjectCommitOptions configures project-specific commits.
type ProjectCommitOptions struct {
	CampaignRoot string        // Path to campaign root
	CampaignID   string        // Campaign ID (truncated to 8 chars)
	Action       ProjectAction // The action performed
	ProjectName  string        // Name of the affected project
	Description  string        // Optional body text
}

// ProjectCommitAll stages all changes and commits with standardized format.
// Commit failures are non-fatal and reported via result.
//
// Commit message format:
//
//	[OBEY-CAMPAIGN-{id}] Action: ProjectName
//
//	Optional description body
func ProjectCommitAll(ctx context.Context, opts ProjectCommitOptions) IntentCommitResult {
	if opts.CampaignRoot == "" || opts.CampaignID == "" {
		return IntentCommitResult{
			Committed: false,
			Message:   "", // Silent failure - campaign info not available
		}
	}

	// Truncate campaign ID to 8 chars
	shortID := opts.CampaignID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	// Build commit message
	commitMsg := fmt.Sprintf("[OBEY-CAMPAIGN-%s] %s: %s",
		shortID,
		opts.Action,
		opts.ProjectName,
	)
	if opts.Description != "" {
		commitMsg += "\n\n" + opts.Description
	}

	// CommitAll has built-in lock handling with retry
	if err := CommitAll(ctx, opts.CampaignRoot, commitMsg); err != nil {
		if errors.Is(err, ErrNoChanges) {
			return IntentCommitResult{
				Committed: false,
				Message:   "(no changes to commit)",
			}
		}
		return IntentCommitResult{
			Committed: false,
			Message:   fmt.Sprintf("Warning: git commit failed: %v", err),
		}
	}

	return IntentCommitResult{
		Committed: true,
		Message:   "Committed changes to git",
	}
}
