package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [project | @shortcut] [command | recipe] [args...]",
	Short: "Execute command from campaign root, or just recipe in a project",
	Long: `Execute any command from the campaign root directory, or run just recipes
in a project directory.

If the first argument exactly matches a project name (a directory in projects/
with a git repo), camp dispatches to 'just' in that project's directory.
Any remaining arguments are passed as the recipe and arguments to just.

If the first argument does not match a project, it is treated as a shell command
and executed from the campaign root directory.

Use @shortcut prefix to run from a shortcut's directory instead of root.
Only navigation shortcuts (those with paths) can be used.

All arguments after 'run' (or '@shortcut') are passed directly to the shell.`,
	Example: `  # Project just dispatch (first arg matches a project name):
  camp run fest              # Show just recipes for fest project
  camp run fest build        # Run 'just build' in projects/fest/
  camp run camp test all     # Run 'just test all' in projects/camp/

  # Raw command from campaign root (first arg is not a project):
  camp run ls -la            # List campaign root contents
  camp run just --list       # Show just recipes from root

  # Shortcut-based execution:
  camp run @p ls             # List projects/ directory
  camp run @f make test      # Run make from festivals/`,
	Aliases:            []string{"r"},
	Args:               cobra.MinimumNArgs(1),
	DisableFlagParsing: true,
	RunE:               runRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.GroupID = "campaign"
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
		sc, ok := cfg.Shortcuts()[shortcutName]
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
			configMappings := buildCategoryMappings(cfg.Shortcuts())

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
					// But only if the next arg after project name is a valid shortcut
					queryParts := strings.Fields(parseResult.Query)
					projectQuery := queryParts[0]
					var subShortcut string
					consumed := 2 // @p + project

					// First resolve the project to check if it has the potential sub-shortcut
					if len(queryParts) > 1 {
						potentialSubShortcut := queryParts[1]
						// Try resolution first to see if the project has this shortcut
						testResult, testErr := index.Resolve(ctx, index.ResolveOptions{
							CampaignRoot: root,
							Category:     parseResult.Category,
							Query:        projectQuery,
						})
						if testErr == nil && testResult.Target != nil && testResult.Target.HasShortcut(potentialSubShortcut) {
							subShortcut = potentialSubShortcut
							consumed = 3 // @p + project + subshortcut
						}
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

					// Determine how many args were consumed
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

	// Project just dispatch: if first arg matches a project, run just in it.
	// Exact match only — projects/<name> must exist and be a git repo.
	if len(commandArgs) > 0 {
		if projectDir, ok := isProject(root, commandArgs[0]); ok {
			return executeCommand(ctx, "just", projectDir, commandArgs[1:])
		}
	}

	if len(commandArgs) == 0 {
		return fmt.Errorf("no command specified")
	}

	// Build the full command string
	fullCmd := strings.Join(commandArgs, " ")

	// Execute from working directory
	return executeCommand(ctx, fullCmd, workDir, nil)
}

// isProject checks if name matches a project directory in projects/<name>
// by verifying the directory exists and contains a .git entry.
func isProject(campaignRoot, name string) (string, bool) {
	projectDir := filepath.Join(campaignRoot, "projects", name)
	info, err := os.Stat(projectDir)
	if err != nil || !info.IsDir() {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".git")); err != nil {
		return "", false
	}
	return projectDir, true
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
			// Propagate the child's exit code through cobra instead of calling
			// os.Exit() directly. This allows deferred cleanup to run and
			// prevents stale file handles in shared test containers.
			return &CommandExitError{Code: exitErr.ExitCode()}
		}
		return fmt.Errorf("failed to execute command: %w", err)
	}

	return nil
}

// CommandExitError signals that a child process exited with a non-zero code.
// main.go checks for this and calls os.Exit with the code, keeping cleanup
// paths intact through cobra's error propagation.
type CommandExitError struct {
	Code int
}

func (e *CommandExitError) Error() string {
	return fmt.Sprintf("command exited with code %d", e.Code)
}
