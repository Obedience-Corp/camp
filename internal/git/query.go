package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Output runs a git command and returns trimmed stdout.
func Output(ctx context.Context, repoPath string, args ...string) (string, error) {
	fullArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

// HasPathDiff reports whether `git diff --quiet -- <path>` sees a change.
func HasPathDiff(ctx context.Context, repoPath, path string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--quiet", "--", path)
	err := cmd.Run()
	if err == nil {
		return false
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return true
	}
	return false
}
