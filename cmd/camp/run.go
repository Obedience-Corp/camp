package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/nav/index"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
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
  camp run camp              # Show just recipes for camp project
  camp run camp test all     # Run 'just test all' in projects/camp/
  camp run festival build    # Run 'just build' in projects/festival/

  # Raw command from campaign root (first arg is not a project):
  camp run just --list       # Show just recipes from root
  camp run git status        # Run git status from campaign root
  camp run ls -la            # List campaign root contents

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
			return camperrors.Wrap(err, "failed to load campaign config")
		}

		// Look up shortcut
		sc, ok := cfg.Shortcuts()[shortcutName]
		if !ok {
			return camperrors.Wrapf(camperrors.ErrNotFound, "unknown shortcut %q (run 'camp shortcuts' to see available shortcuts)", shortcutName)
		}

		// Only navigation shortcuts can be used for directory context
		if !sc.IsNavigation() {
			return camperrors.Wrapf(camperrors.ErrInvalidInput, "shortcut %q is not a navigation shortcut (only shortcuts with paths can be used)", shortcutName)
		}

		// Check if this is a standard path that supports project sub-shortcuts
		// e.g., @p fest cli -> projects/festival-methodology/fest/cmd/fest/
		if nav.IsStandardPath(sc.Path) {
			// Build category mappings from config shortcuts
			configMappings := nav.BuildCategoryMappings(cfg.Shortcuts())

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
							return cmdutil.FormatSubShortcutError(subErr)
						}
						return err
					}

					workDir = resolveResult.Path

					// Determine how many args were consumed
					if consumed >= len(args) {
						return camperrors.Wrap(camperrors.ErrInvalidInput, "no command specified")
					}
					commandArgs = args[consumed:]

					// Verify directory exists
					if stat, err := os.Stat(workDir); err != nil || !stat.IsDir() {
						return camperrors.Wrapf(camperrors.ErrNotFound, "directory does not exist: %s", workDir)
					}

					// Build and execute command
					fullCmd := strings.Join(commandArgs, " ")
					return cmdutil.ExecuteCommand(ctx, fullCmd, workDir, root, nil)
				}
			}
		}

		// Resolve shortcut path to absolute directory (non-project case)
		workDir = filepath.Join(root, sc.Path)

		// Verify directory exists
		if stat, err := os.Stat(workDir); err != nil || !stat.IsDir() {
			return camperrors.Wrapf(camperrors.ErrNotFound, "shortcut directory does not exist: %s", workDir)
		}

		// Remaining args are the command
		commandArgs = args[1:]
	}

	// Project just dispatch: if first arg matches a project, run just in it.
	// Exact match only.
	if len(commandArgs) > 0 {
		if projectDir, ok := isProjectCtx(ctx, root, commandArgs[0]); ok {
			return cmdutil.ExecuteCommand(ctx, "just", projectDir, root, commandArgs[1:])
		}
	}

	if len(commandArgs) == 0 {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "no command specified")
	}

	// Build the full command string
	fullCmd := strings.Join(commandArgs, " ")

	// Execute from working directory
	return cmdutil.ExecuteCommand(ctx, fullCmd, workDir, root, nil)
}

func isProject(campaignRoot, name string) (string, bool) {
	return isProjectCtx(context.Background(), campaignRoot, name)
}

func isProjectCtx(ctx context.Context, campaignRoot, name string) (string, bool) {
	projectDir, err := projectsvc.ResolveByName(ctx, campaignRoot, name)
	if err != nil {
		return "", false
	}
	return projectDir, true
}
