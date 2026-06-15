package cmdutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
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

	// Raw-command form: cmdStr is a shell expression; sh -c is intentional.
	// For argv-safe dispatch use ExecuteDirect.
	cmd := exec.CommandContext(ctx, "sh", "-c", fullCmd)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if campaignRoot != "" {
		cmd.Env = append(cmd.Env, campaign.EnvCampaignRoot+"="+campaignRoot)
	}

	if err := cmd.Run(); err != nil {
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
		if exitErr, ok := err.(*exec.ExitError); ok {
			return camperrors.NewCommand(fullCmd, exitErr.ExitCode(), "", exitErr)
		}
		return camperrors.Wrap(err, "failed to execute command")
	}

	return nil
}
