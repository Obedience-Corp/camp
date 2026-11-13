package git

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Plumbing for deferred commits: capture the index as a tree now, build the
// commit from that tree later.
//
// The tree object is what makes deferral safe. It is immutable and content
// addressed, so a job that records one has captured exactly the snapshot the
// user staged, no matter what happens to the index or the working tree between
// the enqueue and the commit. Running a plain `git commit` later would instead
// sweep in whatever another terminal staged in the meantime.

// WriteTree records the current index as a tree object and returns its SHA.
//
// Cheap, because the index is already built: this writes tree objects for the
// directories it contains and nothing else. It requires a conflict-free index,
// so a repository mid-merge fails here rather than enqueuing a job that could
// never produce a sensible commit.
func WriteTree(ctx context.Context, repoPath string) (string, error) {
	out, err := Output(ctx, repoPath, "write-tree")
	if err != nil {
		return "", camperrors.Wrap(err, "capture the staged tree")
	}
	tree := strings.TrimSpace(out)
	if tree == "" {
		return "", camperrors.New("git write-tree produced no tree")
	}
	return tree, nil
}

// TreeSHA returns the tree object for commitish (a commit SHA, ref, or
// commit^{tree} form). Used to refuse empty deferred commits: when the staged
// tree equals the parent commit's tree there is nothing to land.
//
// Empty commitish is the empty tree (unborn HEAD).
func TreeSHA(ctx context.Context, repoPath, commitish string) (string, error) {
	if strings.TrimSpace(commitish) == "" {
		return EmptyTreeSHA(ctx, repoPath)
	}
	out, err := Output(ctx, repoPath, "rev-parse", commitish+"^{tree}")
	if err != nil {
		return "", camperrors.Wrapf(err, "resolve tree for %s", shortForMessage(commitish))
	}
	tree := strings.TrimSpace(out)
	if tree == "" {
		return "", camperrors.Newf("git rev-parse produced no tree for %s", shortForMessage(commitish))
	}
	return tree, nil
}

func shortForMessage(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// CommitTree creates a commit object for a tree without touching the index,
// the working tree, or any ref.
//
// Plumbing on purpose: it runs no hooks. Repositories with a commit hook camp
// cannot reproduce never reach this path; the enqueue side degrades those to
// a synchronous commit. The fest-direction commit-msg shim does reach it, but
// only after the worker has appended that shim's trailers to the message.
//
// An empty parent makes a root commit (no `-p`): unborn HEAD.
func CommitTree(ctx context.Context, repoPath, tree, parent, message string) (string, error) {
	args := []string{"commit-tree", tree}
	if strings.TrimSpace(parent) != "" {
		args = append(args, "-p", parent)
	}
	args = append(args, "-m", message)
	out, err := Output(ctx, repoPath, args...)
	if err != nil {
		return "", camperrors.Wrap(err, "create the deferred commit object")
	}
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", camperrors.New("git commit-tree produced no commit")
	}
	return sha, nil
}

// UpdateHeadFrom moves HEAD to newSHA, but only if it currently points at
// oldSHA.
//
// The compare and the move are one git operation. That is the whole
// parent-mismatch guarantee: a check followed by a set would leave a window in
// which someone else's commit lands between the two, and camp would then
// silently discard it by pointing HEAD somewhere that never had it as an
// ancestor.
func UpdateHeadFrom(ctx context.Context, repoPath, newSHA, oldSHA, reason string) error {
	expected, err := expectedHEAD(ctx, repoPath, oldSHA)
	if err != nil {
		return err
	}
	if _, err := Output(ctx, repoPath,
		"update-ref", "-m", reason, "HEAD", newSHA, expected); err != nil {
		return camperrors.Wrap(err, "move HEAD to the deferred commit")
	}
	return nil
}

// FirstParentChainContains reports whether HEAD's first-parent chain holds a
// commit whose tree is tree and whose first parent is parent.
//
// Used to recognize a commit by what it contains rather than by its hash. Two
// runs of `git commit-tree` over identical inputs produce different hashes,
// because the committer timestamp is part of the object, so a retry cannot
// identify its own earlier work by SHA. Checking HEAD alone is not enough
// either: the user may have committed on top between the crash and the retry,
// and a landed commit one step down is still landed.
//
// The walk is bounded rather than running to the root. If the sought commit is
// not within the bound, the caller treats the work as not applied and fails
// visibly, which is the recoverable direction: a false "not applied" parks a
// job the user can inspect, while an unbounded walk on a rewritten branch
// could scan the whole history to conclude the same thing.
// TreeChangedPaths lists the paths that differ between two trees.
//
// It exists to describe a deferred commit whose message writer was
// unavailable. The tree pair is the whole description available at that point,
// and it is the accurate one: it names what the commit contains rather than
// what the working tree looks like by the time the worker runs.
//
// An empty from is unborn HEAD, whose "parent" for diff purposes is the empty
// tree; git rejects an empty string as an ambiguous revision.
func TreeChangedPaths(ctx context.Context, repoPath, from, to string) ([]string, error) {
	base := strings.TrimSpace(from)
	if base == "" {
		empty, err := EmptyTreeSHA(ctx, repoPath)
		if err != nil {
			return nil, err
		}
		base = empty
	}
	out, err := Output(ctx, repoPath,
		"diff-tree", "-r", "--name-only", "--no-commit-id", base, to)
	if err != nil {
		return nil, camperrors.Wrap(err, "list the paths a deferred commit changes")
	}
	var paths []string
	for line := range strings.SplitSeq(out, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

func FirstParentChainContains(ctx context.Context, repoPath, tree, parent string) bool {
	out, err := Output(ctx, repoPath, "log", "--first-parent", "--max-count=100",
		"--format=%T %P", "HEAD")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		lineTree := fields[0]
		firstParent := ""
		if len(fields) > 1 {
			firstParent = fields[1]
		}
		if lineTree == tree && firstParent == parent {
			return true
		}
		// Past the recorded parent, nothing older can be this job's commit.
		if firstParent == parent {
			return false
		}
	}
	return false
}

// TreeChangesContained reports whether every path changed from base to wanted
// has exactly the wanted content and mode in current.
//
// A deferred commit leaves its captured tree staged until the worker lands it.
// Another commit can therefore sweep that exact content into history while a
// slow message writer is still running. That fulfills the queue's promise even
// though no commit has the queued tree and parent pair. Comparing whole trees
// would miss that case whenever the later commit also carried unrelated work;
// this projection compares only the paths the queued snapshot changed.
//
// Rename detection is disabled so a rename contributes both its deleted and
// added path. Literal pathspecs preserve unusual filenames, including names
// that begin with ':' and would otherwise be interpreted as pathspec magic.
func TreeChangesContained(ctx context.Context, repoPath, base, wanted, current string) (bool, error) {
	paths, err := treeChangePaths(ctx, repoPath, base, wanted)
	if err != nil {
		return false, err
	}
	return diffQuietAtPaths(ctx, repoPath, []string{"diff", "--quiet", wanted, current, "--"}, paths)
}

func treeChangePaths(ctx context.Context, repoPath, base, wanted string) ([]string, error) {
	cmd := gitCmd(ctx, repoPath, "diff", "--no-renames", "--name-only", "-z", base, wanted)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "list captured tree changes: %s", strings.TrimSpace(stderr.String()))
	}

	rawPaths := bytes.Split(out, []byte{0})
	paths := make([]string, 0, len(rawPaths))
	for _, path := range rawPaths {
		if len(path) == 0 {
			continue
		}
		paths = append(paths, string(path))
	}
	return paths, nil
}

func diffQuietAtPaths(ctx context.Context, repoPath string, prefix, paths []string) (bool, error) {
	if len(paths) == 0 {
		return true, nil
	}
	pathspecs := make([]string, 0, len(paths))
	for _, path := range paths {
		pathspecs = append(pathspecs, ":(literal)"+path)
	}

	// Stay comfortably below the smallest common exec argument limit while
	// avoiding one git process per path for large campaign-root snapshots.
	const maxPathspecBytes = 24 * 1024
	for start := 0; start < len(pathspecs); {
		end, size := start, 0
		for end < len(pathspecs) {
			next := len(pathspecs[end]) + 1
			if end > start && size+next > maxPathspecBytes {
				break
			}
			size += next
			end++
		}

		args := append([]string(nil), prefix...)
		args = append(args, pathspecs[start:end]...)
		compare := gitCmd(ctx, repoPath, args...)
		var compareErr bytes.Buffer
		compare.Stderr = &compareErr
		err := compare.Run()
		if err == nil {
			start = end
			continue
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, camperrors.Wrapf(err, "compare captured tree changes: %s",
			strings.TrimSpace(compareErr.String()))
	}
	return true, nil
}

// FirstParentChainContainsOrSupersedesTreeChanges reports whether current or
// one of its recent first-parent ancestors fulfilled base..wanted.
//
// Exact path-state equality is the strongest evidence. A later commit can also
// supersede the snapshot by versioning every captured path at a newer state.
// Camp accepts that only when none of those paths remains staged relative to
// current: a scratch-index commit leaves the queued snapshot in the real index
// and therefore cannot be mistaken for fulfillment.
//
// Looking through history matters on retry because a path may be versioned by
// one commit and edited or deleted again by the next. Once the later state was
// committed, the deferred job must not demand that an obsolete intermediate
// tree be recreated.
//
// An empty base means the tree was captured against an unborn HEAD (see
// Job.Parent): every path in wanted is "captured", so base is resolved to the
// empty tree before diffing. `git diff` rejects an empty string as an
// ambiguous revision, and the empty tree is what an unborn HEAD's "parent" is
// for diff purposes.
func FirstParentChainContainsOrSupersedesTreeChanges(
	ctx context.Context, repoPath, base, wanted, current string,
) (bool, error) {
	diffBase := base
	if strings.TrimSpace(diffBase) == "" {
		emptyTree, err := EmptyTreeSHA(ctx, repoPath)
		if err != nil {
			return false, err
		}
		diffBase = emptyTree
	}
	capturedPaths, err := treeChangePaths(ctx, repoPath, diffBase, wanted)
	if err != nil {
		return false, err
	}
	if len(capturedPaths) == 0 {
		return true, nil
	}
	indexSettled, err := diffQuietAtPaths(ctx, repoPath,
		[]string{"diff", "--cached", "--quiet", current, "--"}, capturedPaths)
	if err != nil {
		return false, err
	}

	out, err := Output(ctx, repoPath, "rev-list", "--first-parent", "--max-count=100", current)
	if err != nil {
		return false, camperrors.Wrap(err, "walk history for captured tree changes")
	}
	for _, commit := range strings.Fields(out) {
		if commit == base {
			return false, nil
		}
		contained, err := diffQuietAtPaths(ctx, repoPath,
			[]string{"diff", "--quiet", wanted, commit, "--"}, capturedPaths)
		if err != nil {
			return false, err
		}
		if contained {
			return true, nil
		}
		if !indexSettled {
			continue
		}
		committedPaths, err := treeChangePaths(ctx, repoPath, diffBase, commit)
		if err != nil {
			return false, err
		}
		covered := make(map[string]struct{}, len(committedPaths))
		for _, path := range committedPaths {
			covered[path] = struct{}{}
		}
		allCovered := true
		for _, path := range capturedPaths {
			if _, ok := covered[path]; !ok {
				allCovered = false
				break
			}
		}
		if allCovered {
			return true, nil
		}
	}
	return false, nil
}

// HeadSHA returns the commit HEAD points at, or "" when HEAD is unborn or
// unreadable.
//
// Empty rather than an error because both callers treat "cannot tell" and
// "nothing there" the same way, and neither is exceptional: a campaign's first
// commit runs against an unborn HEAD by definition.
func HeadSHA(ctx context.Context, repoPath string) string {
	out, err := Output(ctx, repoPath, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// ResolveHeadSHA returns the commit HEAD points at. When HEAD genuinely has
// no commits yet, it reports unborn=true with sha="" instead of an error: an
// unborn HEAD is a real, expected repository state (a fresh `camp init`), not
// a failure.
//
// Unlike HeadSHA, any other rev-parse failure (corrupt repo, missing .git,
// transient I/O) is returned as err rather than folded into the empty-string
// case. A caller that treats an empty parent as "build a root commit" must
// not do that for a HEAD that is merely unreadable right now: that would
// enqueue a root-commit job against a repository that already has history.
func ResolveHeadSHA(ctx context.Context, repoPath string) (sha string, unborn bool, err error) {
	out, err := Output(ctx, repoPath, "rev-parse", "HEAD")
	if err == nil {
		return strings.TrimSpace(out), false, nil
	}
	if isUnbornHeadRevParseError(err) {
		return "", true, nil
	}
	return "", false, err
}

// isUnbornHeadRevParseError reports whether err is `git rev-parse HEAD`
// failing because HEAD has no commits yet, as opposed to any other reason
// rev-parse could fail. Git's message for the unborn case is stable across
// versions: "unknown revision or path not in the working tree".
func isUnbornHeadRevParseError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unknown revision or path not in the working tree")
}

// StagedFileCount returns how many paths are staged, for the line a deferred
// commit prints in place of a hash.
func StagedFileCount(ctx context.Context, repoPath string) (int, error) {
	out, err := Output(ctx, repoPath, "diff", "--cached", "--name-only")
	if err != nil {
		return 0, camperrors.Wrap(err, "count staged files")
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}
