package git

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

func gitCmd(ctx context.Context, repoPath string, args ...string) *exec.Cmd {
	fullArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	cmd.Env = gitEnv(os.Environ())
	return cmd
}

func gitEnv(base []string) []string {
	env := make([]string, 0, len(base)+2)
	for _, item := range base {
		if strings.HasPrefix(item, "LC_ALL=") || strings.HasPrefix(item, "LANG=") {
			continue
		}
		env = append(env, item)
	}
	return append(env, "LC_ALL=C", "LANG=C")
}
