package git

import (
	"context"
	"errors"
	"fmt"
)

// IntentAction represents the type of intent operation performed.
type IntentAction string

const (
	IntentActionCreate  IntentAction = "Create"
	IntentActionMove    IntentAction = "Move"
	IntentActionArchive IntentAction = "Archive"
	IntentActionDelete  IntentAction = "Delete"
	IntentActionGather  IntentAction = "Gather"
	IntentActionPromote IntentAction = "Promote"
)

// IntentCommitOptions configures intent-specific commits.
type IntentCommitOptions struct {
	CampaignRoot string       // Path to campaign root
	CampaignID   string       // Campaign ID (truncated to 8 chars)
	Action       IntentAction // The action performed
	IntentTitle  string       // Title of the affected intent
	Description  string       // Optional body text
}

// IntentCommitResult contains the outcome of a commit attempt.
type IntentCommitResult struct {
	Committed bool   // True if commit succeeded
	Message   string // User-facing message
}

// IntentCommitAll stages all changes and commits with standardized format.
// Commit failures are non-fatal and reported via result.
//
// Commit message format:
//
//	[OBEY-CAMPAIGN-{id}] Action: Title
//
//	Optional description body
func IntentCommitAll(ctx context.Context, opts IntentCommitOptions) IntentCommitResult {
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
		opts.IntentTitle,
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
