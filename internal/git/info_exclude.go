package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// EnsureInfoExclude adds pattern to the repo's .git/info/exclude if not
// already present. repoRoot is a working tree directory; its actual git dir
// may live elsewhere (worktrees, submodules) and is resolved via git.
//
// The returned bool reports whether the file was actually modified: false
// means the pattern was already present and no write occurred.
func EnsureInfoExclude(ctx context.Context, repoRoot, pattern string) (bool, error) {
	path, err := infoExcludePath(ctx, repoRoot)
	if err != nil {
		return false, err
	}
	return ensurePatternInFile(path, pattern)
}

// RemoveInfoExclude removes pattern from the repo's .git/info/exclude if
// present. Missing files are treated as success.
//
// The returned bool reports whether the pattern was actually removed: false
// means the file did not exist or did not contain the pattern.
func RemoveInfoExclude(ctx context.Context, repoRoot, pattern string) (bool, error) {
	path, err := infoExcludePath(ctx, repoRoot)
	if err != nil {
		return false, err
	}
	return removePatternFromFile(path, pattern)
}

// IsRepo reports whether path is a git working tree (worktree, submodule, or
// plain repo).
func IsRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func infoExcludePath(ctx context.Context, repoRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--git-path", "info/exclude")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(output))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, path)
	}
	return path, nil
}

func ensurePatternInFile(path, pattern string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == pattern {
			return false, nil
		}
	}

	content := strings.TrimRight(string(data), "\n")
	if content != "" {
		content += "\n"
	}
	content += pattern + "\n"
	if err := fsutil.WriteFileAtomically(path, []byte(content), 0644); err != nil {
		return false, err
	}
	return true, nil
}

func removePatternFromFile(path, pattern string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	lines := strings.Split(string(data), "\n")
	filtered := make([]string, 0, len(lines))
	removed := false
	for _, line := range lines {
		if strings.TrimSpace(line) == pattern {
			removed = true
			continue
		}
		filtered = append(filtered, line)
	}
	if !removed {
		return false, nil
	}

	content := strings.TrimRight(strings.Join(filtered, "\n"), "\n")
	if content != "" {
		content += "\n"
	}
	if err := fsutil.WriteFileAtomically(path, []byte(content), 0644); err != nil {
		return false, err
	}
	return true, nil
}
