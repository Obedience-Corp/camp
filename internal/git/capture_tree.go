package git

import (
	"context"
	"os"
	"os/exec"
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
	parts := strings.Split(out, "\x00")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
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
