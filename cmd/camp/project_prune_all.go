package main

import (
	"fmt"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectPruneAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Delete merged branches across all projects",
	Long: `Delete local branches that have been merged into the default branch,
across every project submodule in the campaign.

Produces a per-project summary showing what was (or would be) pruned.

Examples:
  camp project prune all                 # Prune all projects
  camp project prune all --dry-run       # Preview across all projects
  camp project prune all --force         # Skip confirmation for each project
  camp project prune all --remote        # Also prune stale remote tracking refs`,
	RunE: runProjectPruneAll,
}

func init() {
	projectPruneCmd.AddCommand(projectPruneAllCmd)
}

func runProjectPruneAll(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	paths, err := git.ListSubmodulePathsRecursive(ctx, campRoot, "projects/")
	if err != nil {
		return fmt.Errorf("failed to list submodules: %w", err)
	}

	if len(paths) == 0 {
		fmt.Println(ui.Info("No submodules found in this campaign"))
		return nil
	}

	var results []projectPruneResult
	totalDeleted := 0
	totalWouldDelete := 0
	projectsWithWork := 0

	for _, p := range paths {
		fullPath := filepath.Join(campRoot, p)
		name := git.SubmoduleDisplayName(p)

		pr := pruneProject_(ctx, name, fullPath)
		results = append(results, pr)

		for _, r := range pr.Results {
			switch r.Status {
			case "deleted":
				totalDeleted++
			case "would delete":
				totalWouldDelete++
			}
		}
		if len(pr.Results) > 0 || pr.Pruned > 0 || pr.Error != "" {
			projectsWithWork++
		}
	}

	// Render per-project results
	for _, pr := range results {
		if len(pr.Results) == 0 && pr.Pruned == 0 && pr.Error == "" {
			continue // Skip clean projects
		}
		renderPruneResult(pr)
	}

	// Summary
	fmt.Println()
	fmt.Println(ui.Separator(50))
	if pruneDryRun {
		fmt.Printf("%s Would prune %d branch(es) across %d project(s)\n",
			ui.InfoIcon(), totalWouldDelete, projectsWithWork)
	} else if totalDeleted > 0 {
		fmt.Printf("%s Pruned %d branch(es) across %d project(s)\n",
			ui.SuccessIcon(), totalDeleted, projectsWithWork)
	} else {
		fmt.Printf("%s No merged branches to prune across %d project(s)\n",
			ui.SuccessIcon(), len(paths))
	}

	return nil
}
