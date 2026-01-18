package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <command> [args...]",
	Short: "Execute command from campaign root",
	Long: `Execute any command from the campaign root directory.

This is useful when you're deep in a subdirectory but want to run a command
as if you were at the campaign root. The command inherits your current
environment (stdin, stdout, stderr).

All arguments after 'run' are passed directly to the shell.`,
	Example: `  camp run ls -la             # List campaign root contents
  camp run pwd                # Print campaign root path
  camp run just --list        # Show just recipes from root
  camp run make build         # Run make from campaign root
  camp run git commit -m msg  # Run git with flags from root`,
	Aliases:            []string{"r"},
	Args:               cobra.MinimumNArgs(1),
	DisableFlagParsing: true,
	RunE:               runRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Detect campaign root
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	// Build the full command string
	fullCmd := strings.Join(args, " ")

	// Execute from campaign root
	return executeCommand(ctx, fullCmd, root, nil)
}

// executeCommand executes a shell command from the specified directory.
func executeCommand(ctx context.Context, cmdStr string, workDir string, extraArgs []string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Build the full command with extra args
	fullCmd := cmdStr
	if len(extraArgs) > 0 {
		fullCmd = fmt.Sprintf("%s %s", cmdStr, strings.Join(extraArgs, " "))
	}

	// Use sh -c to execute the command (supports pipes, redirects, etc.)
	cmd := exec.CommandContext(ctx, "sh", "-c", fullCmd)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Set environment
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Command failed - exit with the same code
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("failed to execute command: %w", err)
	}

	return nil
}
