package git

import (
	"context"
	"os/exec"
	"strings"
)

// GetUserName retrieves the git user.name from git config.
// It checks local config first, then global config.
// Returns empty string if not configured.
func GetUserName(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "git", "config", "user.name")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
