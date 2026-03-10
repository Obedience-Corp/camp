package main

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var freshAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Run fresh across all project submodules",
	Long: `Run the fresh cycle (checkout default, pull, prune, optional branch)
across every project submodule in the campaign.

Examples:
  camp fresh all                     # Sync all projects
  camp fresh all --branch develop    # Sync all and create develop branch
  camp fresh all --dry-run           # Preview across all projects
  camp fresh all --no-prune          # Sync without pruning`,
	RunE: runFreshAll,
}

func init() {
	freshCmd.AddCommand(freshAllCmd)
}

func runFreshAll(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	paths, err := git.ListSubmodulePathsRecursive(ctx, campRoot, "projects/")
	if err != nil {
		return camperrors.Wrap(err, "failed to list submodules")
	}

	if len(paths) == 0 {
		fmt.Println(ui.Info("No submodules found in this campaign"))
		return nil
	}

	// Load fresh config once
	cfg, err := config.LoadFreshConfig(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "loading fresh config")
	}

	green := lipgloss.NewStyle().Foreground(ui.SuccessColor)
	red := lipgloss.NewStyle().Foreground(ui.ErrorColor)

	fmt.Println(ui.Info("Running fresh across all projects..."))
	fmt.Println()

	var succeeded, failed int
	var failedNames []string

	for _, p := range paths {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		fullPath := filepath.Join(campRoot, p)
		name := git.SubmoduleDisplayName(p)

		// Resolve per-project settings
		branch := cfg.ResolveFreshBranch(freshBranch, freshNoBranch, name)
		doPrune := !freshNoPrune && cfg.ResolveFreshPrune()
		doPush := !freshNoPush && cfg.ResolveFreshPushUpstream(name)

		err := executeFresh(ctx, name, fullPath, freshOptions{
			branch:      branch,
			prune:       doPrune,
			pruneRemote: cfg.ResolveFreshPruneRemote(),
			push:        doPush,
			dryRun:      freshDryRun,
		})
		if err != nil {
			fmt.Printf("  %s %s: %s\n", red.Render("FAILED"), name, err)
			failed++
			failedNames = append(failedNames, name)
		} else {
			succeeded++
		}
	}

	// Summary
	fmt.Println()
	fmt.Println(ui.Separator(50))
	if failed == 0 {
		fmt.Printf("%s Fresh completed for %d project(s)\n", green.Render("All done!"), succeeded)
	} else {
		fmt.Printf("%s %d succeeded, %d failed\n",
			ui.Warning("Fresh completed with errors:"), succeeded, failed)
		for _, name := range failedNames {
			fmt.Printf("  %s %s\n", red.Render("-"), name)
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d project(s) failed", failed)
	}
	return nil
}
