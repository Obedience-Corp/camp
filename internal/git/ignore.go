package git

import (
	"context"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// PathIgnored reports whether path is excluded by gitignore rules in repoPath.
func PathIgnored(ctx context.Context, repoPath, path string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "check-ignore", "-q", "--", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, camperrors.NewGit("check-ignore", "", "", strings.TrimSpace(string(output)), err)
	}
	return true, nil
}
