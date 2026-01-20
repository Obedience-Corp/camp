package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/nav"
	"github.com/obediencecorp/camp/internal/nav/index"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [@shortcut] <command> [args...]",
	Short: "Execute command from campaign root or shortcut directory",
	Long: `Execute any command from the campaign root directory.

This is useful when you're deep in a subdirectory but want to run a command
as if you were at the campaign root. The command inherits your current
environment (stdin, stdout, stderr).

Use @shortcut prefix to run from a shortcut's directory instead of root.
Only navigation shortcuts (those with paths) can be used.

All arguments after 'run' (or '@shortcut') are passed directly to the shell.`,
	Example: `  camp run ls -la             # List campaign root contents
  camp run just --list        # Show just recipes from root
  camp run @p ls              # List projects/ directory
  camp run @f make test       # Run make from festivals/
  camp run @p just build      # Run just from projects/`,
	Aliases:            []string{"r"},
	Args:               cobra.MinimumNArgs(1),
	DisableFlagParsing: true,
	RunE:               runRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.GroupID = "navigation"
}

func runRun(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Detect campaign root
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	// Default working directory is campaign root
	workDir := root
	commandArgs := args

	// Check if first arg is a shortcut reference (@shortcut)
	if len(args) > 0 && strings.HasPrefix(args[0], "@") {
		shortcutName := strings.TrimPrefix(args[0], "@")

		// Load campaign config to get shortcuts
		cfg, _, err := config.LoadCampaignConfigFromCwd(ctx)
		if err != nil {
			return fmt.Errorf("failed to load campaign config: %w", err)
		}

		// Look up shortcut
		sc, ok := cfg.Shortcuts[shortcutName]
		if !ok {
			return fmt.Errorf("unknown shortcut %q (run 'camp shortcuts' to see available shortcuts)", shortcutName)
		}

		// Only navigation shortcuts can be used for directory context
		if !sc.IsNavigation() {
			return fmt.Errorf("shortcut %q is not a navigation shortcut (only shortcuts with paths can be used)", shortcutName)
		}

		// Check if this is a standard path that supports project sub-shortcuts
		// e.g., @p fest cli -> projects/festival-methodology/fest/cmd/fest/
		if isStandardPath(sc.Path) {
			// Build category mappings from config shortcuts
			configMappings := buildCategoryMappings(cfg.Shortcuts)

			// Parse the remaining args to see if there's a project + optional sub-shortcut
			remainingArgs := args[1:]
			if len(remainingArgs) > 0 {
				// Use ParseShortcut to determine if first remaining arg is a query
				// Create a synthetic args list with just the shortcut + potential query
				syntheticArgs := append([]string{shortcutName}, remainingArgs...)
				parseResult := nav.ParseShortcut(syntheticArgs, configMappings)

				// If we have a query, resolve it
				if parseResult.Query != "" {
					// Check for sub-shortcut in query
					queryParts := strings.Fields(parseResult.Query)
					projectQuery := queryParts[0]
					var subShortcut string
					if len(queryParts) > 1 {
						subShortcut = queryParts[1]
					}

					// Resolve the project with sub-shortcut
					resolveResult, err := index.Resolve(ctx, index.ResolveOptions{
						CampaignRoot: root,
						Category:     parseResult.Category,
						Query:        projectQuery,
						SubShortcut:  subShortcut,
					})
					if err != nil {
						// Handle invalid sub-shortcut error
						if subErr, ok := err.(*index.InvalidSubShortcutError); ok {
							return formatSubShortcutError(subErr)
						}
						return err
					}

					workDir = resolveResult.Path

					// Determine how many args were consumed (shortcut + query parts)
					consumed := 1 + len(queryParts) // @p + fest [+ cli]
					if consumed >= len(args) {
						return fmt.Errorf("no command specified")
					}
					commandArgs = args[consumed:]

					// Verify directory exists
					if stat, err := os.Stat(workDir); err != nil || !stat.IsDir() {
						return fmt.Errorf("directory does not exist: %s", workDir)
					}

					// Build and execute command
					fullCmd := strings.Join(commandArgs, " ")
					return executeCommand(ctx, fullCmd, workDir, nil)
				}
			}
		}

		// Resolve shortcut path to absolute directory (non-project case)
		workDir = filepath.Join(root, sc.Path)

		// Verify directory exists
		if stat, err := os.Stat(workDir); err != nil || !stat.IsDir() {
			return fmt.Errorf("shortcut directory does not exist: %s", workDir)
		}

		// Remaining args are the command
		commandArgs = args[1:]
	}

	if len(commandArgs) == 0 {
		return fmt.Errorf("no command specified")
	}

	// Build the full command string
	fullCmd := strings.Join(commandArgs, " ")

	// Execute from working directory
	return executeCommand(ctx, fullCmd, workDir, nil)
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
