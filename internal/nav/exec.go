package nav

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

	// Resolve the category directory
	jumpResult, err := DirectJump(ctx, cat)
	if err != nil {
		return nil, err
	}

	return ExecInDir(ctx, jumpResult.Path, command)
}

// ExecInDir runs a command from the specified directory.
// The command's stdin, stdout, and stderr are connected to the current process.
// Returns the exit result or an error if the command cannot be started.
func ExecInDir(ctx context.Context, dir string, command []string) (*ExecResult, error) {
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

	// Run and capture exit code
	err := cmd.Run()
	exitCode := 0

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// Command failed to start (e.g., not found)
			return nil, &ExecError{
				Command: command[0],
				Dir:     dir,
				Err:     err,
			}
		}
	}

	return &ExecResult{
		ExitCode: exitCode,
		Dir:      dir,
	}, nil
}

// ExecError provides detailed context for execution failures.
type ExecError struct {
	// Command that failed to execute.
	Command string
	// Dir where the command was supposed to run.
	Dir string
	// Err is the underlying error.
	Err error
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("failed to execute '%s' in %s: %v", e.Command, e.Dir, e.Err)
}

func (e *ExecError) Unwrap() error {
	return e.Err
}
