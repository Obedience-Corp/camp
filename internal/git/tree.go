package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

// CommitTree creates a commit object for a tree without touching the index,
// the working tree, or any ref.
//
// Plumbing on purpose: it runs no hooks. Repositories with commit hooks never
// reach this path, because a hook is a user's own code that expects to run at
// commit time in the foreground, and the enqueue side degrades those to a full
// synchronous commit.
func CommitTree(ctx context.Context, repoPath, tree, parent, message string) (string, error) {
	out, err := Output(ctx, repoPath, "commit-tree", tree, "-p", parent, "-m", message)
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
	if _, err := Output(ctx, repoPath,
		"update-ref", "-m", reason, "HEAD", newSHA, oldSHA); err != nil {
		return camperrors.Wrap(err, "move HEAD to the deferred commit")
	}
	return nil
}

// RunWithEnv runs a git command with additional environment variables.
//
// It exists for GIT_INDEX_FILE: the deferred-commit worker materializes a
// captured tree into a scratch index so the message writer sees the snapshot
// being committed rather than the live one. Everything else in this package
// deliberately does not take an environment, because a caller that can set
// arbitrary git variables can also defeat the guarantees the rest of it makes.
func RunWithEnv(ctx context.Context, repoPath string, env []string, args ...string) error {
	cmd := gitCmd(ctx, repoPath, args...)
	cmd.Env = gitEnv(env)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return camperrors.Wrapf(err, "git %s: %s",
			strings.Join(args, " "), strings.TrimSpace(stderr.String()))
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

// IsGitlink reports whether a path is recorded as a submodule pointer.
//
// Mode 160000 is git's gitlink. Read from HEAD rather than the index because
// the index is the user's and a background process must not depend on its
// state; a path that is a submodule in HEAD is a submodule.
func IsGitlink(ctx context.Context, repoPath, path string) bool {
	out, err := Output(ctx, repoPath, "ls-tree", "HEAD", "--", path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(out), "160000 ")
}

// commitHookNames are the hooks that run during an ordinary `git commit`.
//
// Only these three. A repository with a `pre-push` hook can still defer
// commits, because nothing about deferring changes when a push happens.
var commitHookNames = []string{"pre-commit", "prepare-commit-msg", "commit-msg"}

// HasCommitHooks reports whether the repository has an executable hook that an
// ordinary commit would run.
//
// A hook is the user's own code, and it expects to run at commit time, in the
// foreground, against the tree being committed. Deferring past it would either
// skip it silently or run it minutes later against a different working tree,
// and both are worse than not deferring. So a repository with commit hooks
// keeps today's synchronous behavior exactly.
//
// The hooks directory comes from git rather than being assumed, so a repo that
// sets core.hooksPath, or a worktree whose hooks live in the parent's common
// directory, resolves correctly without camp knowing the rules.
func HasCommitHooks(ctx context.Context, repoPath string) bool {
	out, err := Output(ctx, repoPath, "rev-parse", "--git-path", "hooks")
	if err != nil {
		// Cannot tell. Assume hooks exist: the expensive mistake is skipping a
		// hook the user wrote, not committing in the foreground.
		return true
	}
	dir := strings.TrimSpace(out)
	if dir == "" {
		return true
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoPath, dir)
	}

	for _, name := range commitHookNames {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() {
			continue
		}
		// The executable bit is what git itself requires, so a disabled hook
		// left in place as a `.sample` or with its bit cleared does not force
		// the whole repository back to synchronous commits.
		if info.Mode().Perm()&0o111 != 0 {
			return true
		}
	}
	return false
}

// BlobRef is one path's content, captured as a git object.
//
// Capturing content at enqueue is what makes a deferred bookkeeping commit mean
// what its message says. Recording paths alone defers the read as well as the
// write, so an edit made between the two lands under the earlier operation's
// message: the user renames a workitem, edits it, and the rename commit
// contains the edit.
type BlobRef struct {
	// Path is repo-relative.
	Path string
	// Mode is the git file mode ("100644", "100755", "120000").
	Mode string
	// SHA is the blob object. Empty means the path was already gone at
	// capture time and should be removed from the tree.
	SHA string
}

// CaptureBlobs writes each path's current content into the object store and
// returns references to it.
//
// A path that does not exist is captured as a deletion rather than an error,
// because camp's bookkeeping legitimately removes files (a dungeon move, a
// promoted intent) and the commit has to record that.
func CaptureBlobs(ctx context.Context, repoPath string, paths []string) ([]BlobRef, error) {
	refs := make([]BlobRef, 0, len(paths))
	for _, p := range paths {
		abs := filepath.Join(repoPath, filepath.FromSlash(p))
		info, err := os.Lstat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				refs = append(refs, BlobRef{Path: p})
				continue
			}
			return nil, camperrors.Wrapf(err, "capture %s", p)
		}
		if info.IsDir() {
			// A directory expands to its files. Capturing it as one object is
			// not possible, and a deferred job naming a directory means the
			// files under it at the moment the user acted.
			nested, err := captureDir(ctx, repoPath, p)
			if err != nil {
				return nil, err
			}
			refs = append(refs, nested...)
			continue
		}
		sha, err := Output(ctx, repoPath, "hash-object", "-w", "--", p)
		if err != nil {
			return nil, camperrors.Wrapf(err, "capture %s", p)
		}
		refs = append(refs, BlobRef{
			Path: p,
			Mode: blobMode(info),
			SHA:  strings.TrimSpace(sha),
		})
	}
	return refs, nil
}

// captureDir captures every file under a directory.
func captureDir(ctx context.Context, repoPath, dir string) ([]BlobRef, error) {
	var paths []string
	root := filepath.Join(repoPath, filepath.FromSlash(dir))
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(repoPath, p)
		if relErr != nil {
			return relErr
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, camperrors.Wrapf(err, "capture directory %s", dir)
	}
	return CaptureBlobs(ctx, repoPath, paths)
}

// blobMode returns the git mode for a file.
func blobMode(info os.FileInfo) string {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "120000"
	case info.Mode().Perm()&0o111 != 0:
		return "100755"
	default:
		return "100644"
	}
}

// CommitBlobs commits captured content as a scoped commit, without reading the
// working tree.
//
// The temp index starts from HEAD and receives exactly the captured objects, so
// the resulting commit is a function of what was on disk when the job was
// enqueued. Nothing here consults the real index or the current file contents.
func CommitBlobs(ctx context.Context, repoPath string, refs []BlobRef, opts *CommitOptions) error {
	if opts == nil {
		return ErrCommitOptionsRequired
	}
	if len(refs) == 0 {
		return ErrNoChanges
	}

	tmpPath, _, err := BuildTempIndexPath(repoPath)
	if err != nil {
		return err
	}
	defer RemoveTempIndex(tmpPath)

	if err := ReadTreeIntoTempIndex(ctx, repoPath, tmpPath); err != nil {
		return err
	}

	env := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)
	for _, ref := range refs {
		var args []string
		if ref.SHA == "" {
			args = []string{"update-index", "--force-remove", "--", ref.Path}
		} else {
			args = []string{"update-index", "--add", "--cacheinfo",
				ref.Mode + "," + ref.SHA + "," + ref.Path}
		}
		if err := RunWithEnv(ctx, repoPath, env, args...); err != nil {
			return camperrors.Wrapf(err, "stage captured %s", ref.Path)
		}
	}

	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		paths = append(paths, ref.Path)
	}
	changed, err := ExpandTrackedPathsFromTempIndex(ctx, repoPath, tmpPath, paths)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		return ErrNoChanges
	}

	scoped := *opts
	scoped.TempIndexPath = tmpPath
	if err := Commit(ctx, repoPath, &scoped); err != nil {
		return err
	}
	return ResetIndexToHead(ctx, repoPath, changed)
}
