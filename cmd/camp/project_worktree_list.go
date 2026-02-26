package main

import (
	"errors"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
	"github.com/spf13/cobra"
)

var wtListProject string

var projectWorktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List worktrees for the project",
	Long: `List all worktrees for the current project.

Auto-detects the project from your current directory, or use --project
to specify explicitly.

Examples:
  # From within a project
  cd projects/my-api
  camp project worktree list

  # Explicit project
  camp project worktree list --project my-api`,
	RunE: runProjectWorktreeList,
}

func init() {
	projectWorktreeCmd.AddCommand(projectWorktreeListCmd)

	projectWorktreeListCmd.Flags().StringVarP(&wtListProject, "project", "p", "",
		"Project name (auto-detected from cwd if not specified)")
}

func runProjectWorktreeList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Find campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	// Load campaign config
	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		return fmt.Errorf("failed to load campaign config: %w", err)
	}

	// Resolve project name
	resolved, err := project.Resolve(ctx, campRoot, wtListProject)
	if err != nil {
		var notFound *project.ProjectNotFoundError
		if errors.As(err, &notFound) {
			fmt.Println(ui.Dim("\n" + project.FormatProjectList(notFound.AvailableProjects())))
		}
		return err
	}
	projectName := resolved.Name

	// Create resolver and path manager
	resolver := paths.NewResolver(campRoot, cfg.Paths())
	pathManager := worktree.NewPathManager(resolver)

	// Get git worktree list for detailed info
	projectPath := resolver.Project(projectName)
	git := worktree.NewGitWorktree(projectPath)
	entries, err := git.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list worktrees: %w", err)
	}

	// Get worktree names from path manager
	names, err := pathManager.ListProjectWorktrees(projectName)
	if err != nil {
		return fmt.Errorf("failed to list worktree directories: %w", err)
	}

	if len(names) == 0 {
		fmt.Printf("No worktrees for project %s\n", ui.Value(projectName))
		fmt.Println(ui.Dim("Create one with: camp project worktree add <name>"))
		return nil
	}

	fmt.Printf("Worktrees for %s:\n\n", ui.Value(projectName))

	// Build a map of path to entry for lookup
	entryMap := make(map[string]worktree.GitWorktreeEntry)
	for _, e := range entries {
		entryMap[e.Path] = e
	}

	for _, name := range names {
		wtPath := pathManager.WorktreePath(projectName, name)
		relPath := pathManager.RelativeWorktreePath(projectName, name)

		fmt.Printf("  %s\n", ui.Value(name))

		if entry, ok := entryMap[wtPath]; ok {
			fmt.Printf("    Branch: %s\n", ui.Dim(entry.Branch))
			if entry.IsLocked {
				fmt.Printf("    Status: %s\n", ui.Warning("locked"))
			}
		}
		fmt.Printf("    Path:   %s\n", ui.Dim(relPath))
		fmt.Println()
	}

	return nil
}
