package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ChangedNames lists paths that differ from HEAD, staged or not.
//
// Untracked files are absent. An unborn HEAD has no base to diff, so this
// returns no names rather than an error the caller would have to special-case.
func ChangedNames(ctx context.Context, repoPath string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !headResolvable(ctx, repoPath) {
		return nil, nil
	}
	out, err := Output(ctx, repoPath, "diff", "--name-only", "-z", "HEAD")
	if err != nil {
		return nil, camperrors.Wrap(err, "list changes against HEAD")
	}
	return splitNull(out), nil
}

// UntrackedCollapsed lists untracked paths, with untracked directories kept
// as one entry ending in "/".
//
// --directory is the point. A repeat commit must not expand a media tree
// into every file inside it just to learn the directory is still untracked.
func UntrackedCollapsed(ctx context.Context, repoPath string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	out, err := Output(ctx, repoPath, "ls-files", "-z", "-o", "--exclude-standard", "--directory")
	if err != nil {
		return nil, camperrors.Wrap(err, "list untracked paths")
	}
	return splitNull(out), nil
}

func splitNull(out string) []string {
	if out == "" {
		return nil
	}
	// Accept both NUL and newline separators. A command that ignored -z must
	// not collapse the whole listing into one path.
	out = strings.ReplaceAll(out, "\x00", "\n")
	var names []string
	for _, part := range strings.Split(out, "\n") {
		if part != "" {
			names = append(names, part)
		}
	}
	return names
}

// CaptureTree records paths on top of HEAD in a temporary index and returns
// that tree. The repository's real index is not touched.
func CaptureTree(ctx context.Context, repoPath string, paths []string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	tmp, _, err := BuildTempIndexPath(repoPath)
	if err != nil {
		return "", err
	}
	defer RemoveTempIndex(tmp)
	if err := ReadTreeIntoTempIndex(ctx, repoPath, tmp); err != nil {
		return "", err
	}
	if len(paths) > 0 {
		if err := AddPathsToTempIndex(ctx, repoPath, tmp, paths, nil); err != nil {
			return "", err
		}
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "write-tree")
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmp)
	out, err := cmd.Output()
	if err != nil {
		return "", camperrors.Wrap(err, "capture a tree without staging")
	}
	tree := strings.TrimSpace(string(out))
	if tree == "" {
		return "", camperrors.New("git write-tree produced no tree")
	}
	return tree, nil
}

// InstallTree makes the real index match tree.
//
// The commit-tree worker only moves HEAD. It does not touch the index, and
// its contract is that the index already holds this snapshot. finish(false)
// puts the previous index back when the job could not be queued. finish(true)
// drops the backup and keeps the snapshot.
func InstallTree(ctx context.Context, repoPath, tree string) (finish func(keep bool) error, err error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	gitDir, err := ResolveGitDir(repoPath)
	if err != nil {
		return nil, err
	}
	real := filepath.Join(gitDir, "index")
	backup := real + ".camp-before-queue"
	if _, statErr := os.Stat(real); statErr == nil {
		if err := CopyFile(real, backup); err != nil {
			return nil, err
		}
	}
	finish = func(keep bool) error {
		if keep {
			if removeErr := os.Remove(backup); removeErr != nil && !os.IsNotExist(removeErr) {
				return removeErr
			}
			return nil
		}
		if _, statErr := os.Stat(backup); statErr != nil {
			return nil
		}
		return os.Rename(backup, real)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "read-tree", tree)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		_ = finish(false)
		return nil, camperrors.Wrapf(runErr, "install captured tree into the index: %s", strings.TrimSpace(string(out)))
	}
	return finish, nil
}

// RelocatedIntoRoots returns untracked files under roots that are the same
// blob as a tracked path deleted in the worktree.
//
// A move into a declared root is still that tracked file. Leaving the new
// path out records a deletion and drops the content. Listing is limited to
// basename matches, and only runs when something was deleted, so a commit
// that does not move a file does not walk the root.
func RelocatedIntoRoots(ctx context.Context, repoPath string, roots []string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(roots) == 0 || !headResolvable(ctx, repoPath) {
		return nil, nil
	}
	deleted, err := deletedNames(ctx, repoPath)
	if err != nil || len(deleted) == 0 {
		return nil, err
	}
	blobs := make(map[string]struct{}, len(deleted))
	bases := make(map[string]struct{}, len(deleted))
	for _, path := range deleted {
		blob, blobErr := Output(ctx, repoPath, "rev-parse", "HEAD:"+path)
		if blobErr != nil {
			continue
		}
		blobs[blob] = struct{}{}
		bases[filepath.Base(path)] = struct{}{}
	}
	if len(blobs) == 0 {
		return nil, nil
	}
	var moved []string
	seen := make(map[string]struct{})
	for _, root := range roots {
		out, lsErr := Output(ctx, repoPath, "ls-files", "-z", "-o", "--exclude-standard", "--", strings.TrimSuffix(root, "/"))
		if lsErr != nil {
			return nil, lsErr
		}
		for _, path := range splitNull(out) {
			if path == "" || strings.HasSuffix(path, "/") {
				continue
			}
			if _, ok := bases[filepath.Base(path)]; !ok {
				continue
			}
			if _, ok := seen[path]; ok {
				continue
			}
			blob, hashErr := Output(ctx, repoPath, "hash-object", "--", filepath.Join(repoPath, filepath.FromSlash(path)))
			if hashErr != nil {
				continue
			}
			if _, ok := blobs[blob]; !ok {
				continue
			}
			seen[path] = struct{}{}
			moved = append(moved, path)
		}
	}
	return moved, nil
}

func deletedNames(ctx context.Context, repoPath string) ([]string, error) {
	// Compare to HEAD, not the index. By the time this runs, git add may
	// already have staged the deletion, and ls-files -d would then be empty.
	out, err := Output(ctx, repoPath, "diff", "--name-only", "--diff-filter=D", "HEAD")
	if err != nil {
		return nil, camperrors.Wrap(err, "list deleted paths")
	}
	return splitNull(out), nil
}
