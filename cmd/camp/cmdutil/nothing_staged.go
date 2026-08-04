package cmdutil

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/git"
)

// NothingStagedLine is the no-op message after a staged-only commit gate finds
// nothing to commit.
//
// The gate checks staged changes only, so claiming "working tree clean" is
// wrong when unstaged or untracked dirt remains (common at campaign root with
// excluded submodule pointers). scope is a trailing place qualifier such as
// " in project" or " in worktree"; empty for campaign-root commit.
func NothingStagedLine(ctx context.Context, repoPath, scope string) string {
	// hasStaged is already known false by the caller. HasChanges here is
	// unstaged + untracked only in practice, and is the same dirt the user
	// still sees in git status.
	dirty, err := git.HasChanges(ctx, repoPath)
	if err != nil || dirty {
		return "Nothing staged to commit" + scope
	}
	if scope == "" {
		return "Nothing to commit, working tree clean"
	}
	return "Nothing to commit" + scope
}
