package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// mergeTreeConflict is git merge-tree's exit code for "merged, with conflicts".
// Anything above it is a real failure, including 129 for a git too old to know
// --write-tree at all.
const mergeTreeConflict = 1

// ReapplyTreeOnto re-applies the change between base and tree on top of onto,
// and returns the merged tree object.
//
// This is a cherry-pick, not a re-parent, and the difference is the whole
// reason the function exists. A captured tree is a whole-repository snapshot,
// so hanging it off a newer commit would publish the snapshot's version of
// every path — silently reverting whatever landed in between. Merging with
// base as the merge base applies only what the snapshot changed, which is the
// commit the same work would have produced had it run in the foreground after
// the intervening commit instead of before it.
//
// An empty base is unborn HEAD: the snapshot was queued as a root commit, so
// its "change" is measured from the empty tree.
//
// Nothing here touches the index or the working tree. git merge-tree writes
// the merged tree straight to the object store, which is what makes this safe
// to run from a detached worker against a repository the user is still
// editing.
//
// A conflict returns ErrReapplyConflict naming the paths that conflicted and
// wrapping git's own report. Callers must treat that as terminal: a conflicted
// merge tree carries conflict markers and stage entries, and committing it
// would put them in history.
func ReapplyTreeOnto(ctx context.Context, repoPath, base, tree, onto string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	mergeBase := strings.TrimSpace(base)
	if mergeBase == "" {
		empty, err := EmptyTreeSHA(ctx, repoPath)
		if err != nil {
			return "", err
		}
		mergeBase = empty
	}

	cmd := gitCmd(ctx, repoPath, "merge-tree", "--write-tree",
		"--merge-base="+mergeBase, onto, tree)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()

	merged, conflicted, report := splitMergeTreeOutput(string(out))
	if runErr == nil {
		if merged == "" {
			return "", camperrors.New("git merge-tree produced no tree")
		}
		return merged, nil
	}

	var exitErr *exec.ExitError
	if camperrors.As(runErr, &exitErr) && exitErr.ExitCode() == mergeTreeConflict {
		return "", camperrors.WrapJoinf(ErrReapplyConflict, runErr,
			"re-applying the captured changes onto %s conflicted %s: %s",
			shortForMessage(onto), conflictSummary(conflicted), report)
	}
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = report
	}
	return "", camperrors.Wrapf(runErr, "re-apply the captured changes onto %s: %s",
		shortForMessage(onto), detail)
}

// splitMergeTreeOutput separates merge-tree's tree OID, the paths that
// conflicted, and the human-readable report.
//
// The first line is the tree, whether or not the merge conflicted. On a
// conflict, stage entries for every conflicted path follow it, and git's own
// "CONFLICT (…)" lines come after a blank line.
//
// The stage entries are parsed rather than the report, even though the report
// also names the paths, because only the entries are a list. The report leads
// with "Auto-merging <path>" lines for paths that merged fine, so a failure
// bounded to a few hundred bytes — which is what a recorded job error is —
// spends its whole budget on the paths that were not the problem and truncates
// before reaching the ones that were. That is exactly what happened to the
// failure this parsing was added for.
func splitMergeTreeOutput(out string) (tree string, conflicted []string, report string) {
	block, rest, _ := strings.Cut(out, "\n\n")
	lines := strings.Split(strings.TrimSpace(block), "\n")
	return strings.TrimSpace(lines[0]), conflictedPaths(lines[1:]), strings.TrimSpace(rest)
}

// conflictedPaths reads the paths out of merge-tree's stage entries.
//
// Each entry is "<mode> <oid> <stage>\t<path>", and a conflicted path appears
// once per stage it has, so the same path arrives up to three times. Order is
// git's, deduplicated, because that is the order the report discusses them in.
func conflictedPaths(entries []string) []string {
	var paths []string
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		_, path, ok := strings.Cut(entry, "\t")
		if !ok || path == "" {
			continue
		}
		if _, dup := seen[path]; dup {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

// conflictNameBudget bounds how many paths a conflict failure names outright.
//
// The list exists so the user can act on it, and the job error carrying it is
// truncated to a few hundred bytes before it reaches `camp jobs`. Naming a few
// and counting the rest keeps the actionable part inside that budget; naming
// all of a hundred would push every one of them past it.
const conflictNameBudget = 6

// conflictSummary renders conflicted paths for a failure a person reads.
//
// No paths produces the unqualified phrase rather than a claim about zero
// files: git reported a conflict, so something conflicted, and saying "in 0
// files" would contradict the failure it is attached to.
func conflictSummary(paths []string) string {
	switch {
	case len(paths) == 0:
		return "(camp could not determine which paths)"
	case len(paths) == 1:
		return "in " + paths[0]
	case len(paths) <= conflictNameBudget:
		return fmt.Sprintf("in %d files (%s)", len(paths), strings.Join(paths, ", "))
	default:
		return fmt.Sprintf("in %d files (%s, and %d more)", len(paths),
			strings.Join(paths[:conflictNameBudget], ", "), len(paths)-conflictNameBudget)
	}
}
