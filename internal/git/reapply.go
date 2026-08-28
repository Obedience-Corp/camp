package git

import (
	"bytes"
	"context"
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
// A conflict returns ErrReapplyConflict wrapping git's own report. Callers
// must treat that as terminal: a conflicted merge tree carries conflict
// markers and stage entries, and committing it would put them in history.
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

	merged, report := splitMergeTreeOutput(string(out))
	if runErr == nil {
		if merged == "" {
			return "", camperrors.New("git merge-tree produced no tree")
		}
		return merged, nil
	}

	var exitErr *exec.ExitError
	if camperrors.As(runErr, &exitErr) && exitErr.ExitCode() == mergeTreeConflict {
		return "", camperrors.WrapJoinf(ErrReapplyConflict, runErr,
			"re-applying the captured changes onto %s conflicted: %s",
			shortForMessage(onto), report)
	}
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = report
	}
	return "", camperrors.Wrapf(runErr, "re-apply the captured changes onto %s: %s",
		shortForMessage(onto), detail)
}

// splitMergeTreeOutput separates merge-tree's tree OID from the human-readable
// report that follows it.
//
// The first line is the tree, whether or not the merge conflicted. On a
// conflict the stage entries and git's own "CONFLICT (…)" lines follow, split
// from the OID block by a blank line. The report is carried into the error
// verbatim because git already says which paths conflicted and how, and
// re-deriving that from the stage entries would say it worse.
func splitMergeTreeOutput(out string) (tree, report string) {
	block, rest, _ := strings.Cut(out, "\n\n")
	lines := strings.SplitN(strings.TrimSpace(block), "\n", 2)
	tree = strings.TrimSpace(lines[0])
	return tree, strings.TrimSpace(rest)
}
