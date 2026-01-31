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

// GetUserEmail retrieves the git user.email from git config.
// It checks local config first, then global config.
// Returns empty string if not configured.
func GetUserEmail(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "git", "config", "user.email")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// GetUserIdentity returns both name and email formatted as "Name <email>".
// Returns empty string if name is not configured.
func GetUserIdentity(ctx context.Context) string {
	name := GetUserName(ctx)
	if name == "" {
		return ""
	}

	email := GetUserEmail(ctx)
	if email != "" {
		return name + " <" + email + ">"
	}
	return name
}
