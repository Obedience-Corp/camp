package cmdutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/shellcmd"
)

// ExecuteCommand executes a shell command from the specified directory.
// campaignRoot is threaded by callers that already resolved campaign context,
// avoiding a second detect pass in execution hot paths.
func ExecuteCommand(ctx context.Context, cmdStr, workDir, campaignRoot string, extraArgs []string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	fullCmd := cmdStr
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		var err error
		cmd, err = shellcmd.CommandInDir(ctx, "powershell", cmdStr, extraArgs, workDir)
		if err != nil {
			return camperrors.Wrap(err, "resolving PowerShell command interpreter")
		}
	} else {
		// Preserve the existing raw-expression contract outside Windows.
		if len(extraArgs) > 0 {
			fullCmd = fmt.Sprintf("%s %s", cmdStr, strings.Join(extraArgs, " "))
		}
		cmd = exec.CommandContext(ctx, "sh", "-c", fullCmd)
		cmd.Dir = workDir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if campaignRoot != "" {
		cmd.Env = append(cmd.Env, campaign.EnvCampaignRoot+"="+campaignRoot)
	}

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return camperrors.Wrap(ctx.Err(), "command cancelled")
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return camperrors.NewCommand(fullCmd, exitErr.ExitCode(), "", exitErr)
		}
		return camperrors.Wrap(err, "failed to execute command")
	}

	return nil
}

// ExecuteDirect executes binary with argv directly (no shell).
// Use this for dispatch paths where arguments must survive byte-for-byte.
func ExecuteDirect(ctx context.Context, binary string, args []string, workDir, campaignRoot string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	argv := append([]string{binary}, args...)
	fullCmd := strings.Join(argv, " ")

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if campaignRoot != "" {
		cmd.Env = append(cmd.Env, campaign.EnvCampaignRoot+"="+campaignRoot)
	}

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return camperrors.Wrap(ctx.Err(), "command cancelled")
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return camperrors.NewCommand(fullCmd, exitErr.ExitCode(), "", exitErr)
		}
		return camperrors.Wrap(err, "failed to execute command")
	}

	return nil
}
