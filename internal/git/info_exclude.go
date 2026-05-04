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
func EnsureInfoExclude(ctx context.Context, repoRoot, pattern string) error {
	path, err := infoExcludePath(ctx, repoRoot)
	if err != nil {
		return err
	}
	return ensurePatternInFile(path, pattern)
}

// RemoveInfoExclude removes pattern from the repo's .git/info/exclude if
// present. Missing files are treated as success.
func RemoveInfoExclude(ctx context.Context, repoRoot, pattern string) error {
	path, err := infoExcludePath(ctx, repoRoot)
	if err != nil {
		return err
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

func ensurePatternInFile(path, pattern string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}

	content := strings.TrimRight(string(data), "\n")
	if content != "" {
		content += "\n"
	}
	content += pattern + "\n"
	return fsutil.WriteFileAtomically(path, []byte(content), 0644)
}

func removePatternFromFile(path, pattern string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == pattern {
			continue
		}
		filtered = append(filtered, line)
	}

	content := strings.TrimRight(strings.Join(filtered, "\n"), "\n")
	if content != "" {
		content += "\n"
	}
	return fsutil.WriteFileAtomically(path, []byte(content), 0644)
}
