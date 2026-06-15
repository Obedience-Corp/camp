package main

import (
	"context"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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

Raw command arguments after 'run' (or '@shortcut') are passed directly to the
shell. Project just-dispatch passes recipe arguments directly to just.`,
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

		resolution, err := index.ResolveRunShortcut(ctx, root, cfg, shortcutName, args[1:])
		if err != nil {
			return err
		}
		workDir = resolution.WorkDir
		commandArgs = resolution.CommandArgs
		if resolution.BypassProjectDispatch {
			fullCmd := strings.Join(commandArgs, " ")
			return cmdutil.ExecuteCommand(ctx, fullCmd, workDir, root, nil)
		}
	}

	// Project just dispatch: if first arg matches a project, run just in it.
	// Exact match only.
	if len(commandArgs) > 0 {
		if projectDir, ok := isProjectCtx(ctx, root, commandArgs[0]); ok {
			return cmdutil.ExecuteDirect(ctx, "just", commandArgs[1:], projectDir, root)
		}
	}

	if len(commandArgs) == 0 {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "no command specified")
	}

	// Raw-command form: shell interprets the joined args; shell metacharacters are intentional.
	fullCmd := strings.Join(commandArgs, " ")

	// Execute from working directory
	return cmdutil.ExecuteCommand(ctx, fullCmd, workDir, root, nil)
}

func isProjectCtx(ctx context.Context, campaignRoot, name string) (string, bool) {
	projectDir, err := projectsvc.ResolveByName(ctx, campaignRoot, name)
	if err != nil {
		return "", false
	}
	return projectDir, true
}
