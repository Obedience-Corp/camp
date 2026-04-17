package nav

import (
	"context"
	"errors"
	"os"
	"os/exec"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ExecResult contains the result of command execution.
type ExecResult struct {
	// ExitCode is the exit code of the executed command.
	ExitCode int
	// Dir is the directory where the command was executed.
	Dir string
}

// ErrNoCommand is returned when no command is provided.
var ErrNoCommand = errors.New("no command provided")

// ExecInCategory runs a command from the specified category directory.
// The command's stdin, stdout, and stderr are connected to the current process.
// Returns the exit result or an error if the command cannot be started.
func ExecInCategory(ctx context.Context, cat Category, command []string) (*ExecResult, error) {
	if len(command) == 0 {
		return nil, ErrNoCommand
	}

	campaignRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, err
	}

	// Resolve the category directory
	jumpResult, err := DirectJumpFromRoot(ctx, campaignRoot, cat)
	if err != nil {
		return nil, err
	}

	return ExecInDir(ctx, jumpResult.Path, campaignRoot, command)
}

// ExecInDir runs a command from the specified directory with a caller-supplied
// campaign root. The command's stdin, stdout, and stderr are connected to the
// current process. Pass an empty campaignRoot if no CAMP_ROOT env var should be
// set. Returns the exit result or an error if the command cannot be started.
func ExecInDir(ctx context.Context, dir, campaignRoot string, command []string) (*ExecResult, error) {
	if len(command) == 0 {
		return nil, ErrNoCommand
	}

	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Create command
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if campaignRoot != "" {
		cmd.Env = append(cmd.Env, campaign.EnvCampaignRoot+"="+campaignRoot)
	}

	// Run and capture exit code
	err := cmd.Run()
	exitCode := 0

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// Command failed to start (e.g., not found)
			return nil, camperrors.NewCommand(command[0], 0, "", err)
		}
	}

	return &ExecResult{
		ExitCode: exitCode,
		Dir:      dir,
	}, nil
}
