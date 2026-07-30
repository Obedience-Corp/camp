package cmdutil

import (
	"context"
	"fmt"
	"io"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/stageguard"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// What the user sees when a commit stages nothing.
//
// The staging guard lives at the staging chokepoint, so `camp commit
// --all=false` over an index the user built with git add never reaches it: a
// 13 MB PNG could enter a commit without camp saying a word. The report below
// is the whole of the fix.
//
// Report-only is the design, not a first increment. Camp's commit default is
// "commit everything without having to think about it", and a guard that
// excluded or asked here would turn an index the user assembled deliberately
// back into a decision point, which is what the default exists to avoid. Acting
// on the contents would also mean reversing an explicit `git add` -- the one
// thing the staging guard already refuses to do for hand-staged files, since it
// excludes by staging around a path and cannot un-add what is already in.
//
// So the commit that follows this report is byte-for-byte the commit that would
// have happened without it. What changes is only that the user is told.

// ReportStagedGrowth writes the guard's findings for an index camp did not
// stage, and returns them. It excludes nothing, unstages nothing, blocks
// nothing, and prompts for nothing.
//
// commitLarge is the user having already answered this. The staging guard
// suppresses detection entirely under --commit-large rather than
// detecting-then-ignoring, and so does this, so the two agree about what the
// flag means.
//
// It is fail-open, like the guard at the staging chokepoint: a repository camp
// cannot read still commits. The cause is printed rather than swallowed,
// because the dangerous way to degrade is the quiet one -- a user who is not
// told assumes the check ran.
func ReportStagedGrowth(
	ctx context.Context,
	out io.Writer,
	repoPath string,
	commitLarge bool,
) []stageguard.GuardViolation {
	if commitLarge {
		return nil
	}

	violations, limits, err := stagedGrowth(ctx, repoPath)
	if err != nil {
		_, _ = fmt.Fprintln(out, ui.Warning(StagedCheckUnavailableLine(err)))
		return nil
	}
	// Same renderer as the staging path. A user who hits this over a hand-built
	// index and a user who hits it through `camp commit` are looking at the same
	// situation, and must not have to notice that the wording differs.
	for _, v := range violations {
		renderTrackedGrowth(out, v, limits)
	}
	return violations
}

// stagedGrowth resolves the repository's guard policy and evaluates the index
// against it.
func stagedGrowth(
	ctx context.Context, repoPath string,
) ([]stageguard.GuardViolation, stageguard.GuardLimits, error) {
	limits, err := stageguard.ResolveLimits(ctx, repoPath)
	if err != nil {
		return nil, limits, camperrors.Wrapf(err, "resolve staging guard limits for %s", repoPath)
	}
	violations, err := stageguard.CheckStagedIndex(ctx, repoPath, limits)
	if err != nil {
		return nil, limits, camperrors.Wrapf(err, "evaluate staged files in %s", repoPath)
	}
	stageguard.SortViolations(violations)
	return violations, limits, nil
}

// StagedCheckUnavailableLine says the size check did not run over an index camp
// did not stage.
//
// It names the cause for the same reason GuardUnavailableLine does: the
// realistic causes need different responses from the user, and a bare "check
// unavailable" gives them nothing to act on.
//
// Every clause is in a tense that is true at the moment it prints, and none of
// them claims a commit. This runs before the commit object is written, and the
// flow after it can still end in "nothing to commit", a refusal over
// pre-staged submodule refs, a deferred message writer, or a git failure.
// GuardUnavailableLine can afford "Staged without..." because staging really
// has happened by then; the equivalent past tense here would announce an
// outcome camp does not yet know, which is the same wrong-by-assertion problem
// the line exists to avoid.
func StagedCheckUnavailableLine(cause error) string {
	return fmt.Sprintf(
		"Could not run the size check over the staged files: %v\n"+
			"  Large files were not checked, and camp does not stop for that.\n"+
			"  Run 'camp doctor -c bigfiles' if you want to look.",
		cause)
}
