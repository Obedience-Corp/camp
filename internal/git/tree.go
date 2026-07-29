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
