package cmdutil

import (
	"context"
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/defercommit"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// What the user sees when an --auto-write commit defers.
//
// The whole point is that their terminal comes back before the message writer
// runs, which means the commit does not exist yet when they read this line. So
// the line has to be honest about that without being alarming: it says the work
// is captured and where to look, because a user who thinks a commit landed and
// finds no hash has been lied to by omission.

// TryDeferAutoWrite defers an --auto-write commit when it is safe to.
//
// Returns true when the commit was queued and the caller should return without
// committing. False means the caller proceeds exactly as before: the refusal
// reasons are all cases where synchronous is the correct behavior, not
// failures, so nothing is reported when it declines.
//
// opts carries what the worker cannot re-derive: the writer's environment, the
// campaign tag, and any follow-up. All three depend on where the user ran the
// command, and a deferred commit that dropped one would silently differ from
// the commit its synchronous twin makes.
//
// Staging has already happened in the foreground when this runs. That ordering
// matters: the guard, the artifact-root declaration, and the user's own
// decisions all still happen while they are watching. Only the message writing
// and the commit object move to the background.
func TryDeferAutoWrite(ctx context.Context, w io.Writer, campaignRoot, repoPath string, jsonOut, amend bool, opts defercommit.EnqueueOptions) (bool, error) {
	allowed, _ := defercommit.Allowed(ctx, defercommit.Request{
		CampaignRoot: campaignRoot,
		RepoPath:     repoPath,
		JSON:         jsonOut,
		Amend:        amend,
	})
	if !allowed {
		return false, nil
	}

	if _, err := defercommit.Enqueue(ctx, campaignRoot, repoPath, opts); err != nil {
		// Deferring is an optimization over a path that still works. A queue
		// that cannot be written should not cost the user their commit, so
		// this degrades to the synchronous path and says why.
		_, _ = fmt.Fprintln(w, ui.Warning(
			fmt.Sprintf("Could not queue this commit (%v); writing the message now.", err)))
		return false, nil
	}

	staged, countErr := git.StagedFileCount(ctx, repoPath)
	if countErr != nil {
		staged = 0
	}
	_, _ = fmt.Fprintln(w, ui.Success(DeferredCommitLine(staged)))
	_, _ = fmt.Fprintln(w, ui.Dim("  camp jobs   see it land"))
	return true, nil
}

// DeferredCommitLine is the one line a deferred --auto-write commit prints.
//
// It says "queued" rather than "committed" because the commit does not exist
// yet, and it names the writer as the reason so the wait is explicable rather
// than mysterious.
func DeferredCommitLine(staged int) string {
	noun := "files"
	if staged == 1 {
		noun = "file"
	}
	if staged <= 0 {
		return "Staged and queued (writing the commit message in the background)"
	}
	return fmt.Sprintf("%d %s staged and queued (writing the commit message in the background)",
		staged, noun)
}
