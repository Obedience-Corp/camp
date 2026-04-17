package cmdutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ExecuteCommand executes a shell command from the specified directory.
// campaignRoot is threaded by callers that already resolved campaign context,
// avoiding a second detect pass in execution hot paths.
func ExecuteCommand(ctx context.Context, cmdStr, workDir, campaignRoot string, extraArgs []string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	fullCmd := cmdStr
	if len(extraArgs) > 0 {
		fullCmd = fmt.Sprintf("%s %s", cmdStr, strings.Join(extraArgs, " "))
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", fullCmd)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if campaignRoot != "" {
		cmd.Env = append(cmd.Env, "CAMP_ROOT="+campaignRoot)
	}

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return camperrors.NewCommand(fullCmd, exitErr.ExitCode(), "", exitErr)
		}
		return camperrors.Wrap(err, "failed to execute command")
	}

	return nil
}
