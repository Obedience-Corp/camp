package git

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Output runs a git command and returns trimmed stdout.
func Output(ctx context.Context, repoPath string, args ...string) (string, error) {
	cmd := gitCmd(ctx, repoPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", camperrors.Wrapf(err, "git %s: %s", strings.Join(args, " "), detail)
		}
		return "", camperrors.Wrapf(err, "git %s", strings.Join(args, " "))
	}
	return strings.TrimSpace(string(output)), nil
}

// HasPathDiff reports whether `git diff --quiet -- <path>` sees a change.
func HasPathDiff(ctx context.Context, repoPath, path string) bool {
	cmd := gitCmd(ctx, repoPath, "diff", "--quiet", "--", path)
	err := cmd.Run()
	if err == nil {
		return false
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return true
	}
	return false
}
