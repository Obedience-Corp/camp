package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <shortcut> [args...]",
	Short: "Execute a command shortcut",
	Long: `Execute a command shortcut defined in .campaign/campaign.yaml.

Command shortcuts allow you to define frequently-used commands that should be
executed from specific directories within your campaign. Extra arguments are
passed through to the command.

Define shortcuts in .campaign/campaign.yaml:

  shortcuts:
    build:
      command: "just build"
      description: "Build all projects"
    dev:
      command: "docker compose up -d"
      workdir: "dev/infrastructure"
      description: "Start dev environment"

The command is executed from the campaign root by default, or from 'workdir'
if specified. The working directory path is relative to the campaign root.`,
	Example: `  camp run build              # Run build shortcut
  camp run dev                # Start dev environment
  camp run test -- --verbose  # Pass args to test command`,
	Aliases: []string{"r"},
	Args:    cobra.MinimumNArgs(1),
	RunE:    runRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Load campaign config
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return err
	}

	shortcutName := args[0]
	extraArgs := args[1:]

	// Look up the shortcut
	sc, ok := cfg.Shortcuts[shortcutName]
	if !ok {
		return fmt.Errorf("shortcut %q not found\n\nRun 'camp shortcuts' to see available shortcuts", shortcutName)
	}

	// Verify this is a command shortcut
	if !sc.IsCommand() {
		if sc.IsNavigation() {
			return fmt.Errorf("shortcut %q is a navigation shortcut (use 'camp go %s' instead)", shortcutName, shortcutName)
		}
		return fmt.Errorf("shortcut %q has no command defined", shortcutName)
	}

	// Determine working directory
	workDir := campaignRoot
	if sc.WorkDir != "" {
		workDir = filepath.Join(campaignRoot, sc.WorkDir)
		// Verify the directory exists
		if stat, err := os.Stat(workDir); err != nil || !stat.IsDir() {
			return fmt.Errorf("working directory %q does not exist", sc.WorkDir)
		}
	}

	// Execute the command
	return executeCommand(ctx, sc.Command, workDir, extraArgs)
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
