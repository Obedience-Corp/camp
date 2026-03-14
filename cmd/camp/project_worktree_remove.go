package main

import (
	"errors"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	projectcmd "github.com/Obedience-Corp/camp/cmd/camp/project"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	wtRemoveProject string
	wtRemoveForce   bool
)

var projectWorktreeRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a worktree",
	Long: `Remove a worktree from the current project.

Auto-detects the project from your current directory, or use --project
to specify explicitly.

Examples:
  # From within a project
  cd projects/my-api
  camp project worktree remove feature-auth

  # Force remove (even with uncommitted changes)
  camp project worktree remove experiment --force

  # Explicit project
  camp project worktree remove feature --project my-api`,
	Args: cobra.ExactArgs(1),
	RunE: runProjectWorktreeRemove,
}

func init() {
	projectWorktreeCmd.AddCommand(projectWorktreeRemoveCmd)

	projectWorktreeRemoveCmd.Flags().StringVarP(&wtRemoveProject, "project", "p", "",
		"Project name (auto-detected from cwd if not specified)")
	projectWorktreeRemoveCmd.Flags().BoolVarP(&wtRemoveForce, "force", "f", false,
		"Force removal even with uncommitted changes")

	if err := projectWorktreeRemoveCmd.RegisterFlagCompletionFunc("project", projectcmd.CompleteProjectName); err != nil {
		panic(err)
	}
}

func runProjectWorktreeRemove(cmd *cobra.Command, args []string) error {
	worktreeName := args[0]

	ctx := cmd.Context()

	// Find campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	// Load campaign config
	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "failed to load campaign config")
	}

	// Resolve project name
	resolved, err := project.Resolve(ctx, campRoot, wtRemoveProject)
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

	// Check worktree exists
	if !pathManager.WorktreeExists(projectName, worktreeName) {
		return fmt.Errorf("worktree '%s' does not exist for project '%s'", worktreeName, projectName)
	}

	// Get worktree path
	wtPath := pathManager.WorktreePath(projectName, worktreeName)

	// Remove via git
	projectPath := resolver.Project(projectName)
	git := worktree.NewGitWorktree(projectPath)

	if err := git.Remove(ctx, wtPath, wtRemoveForce); err != nil {
		return camperrors.Wrap(err, "failed to remove worktree")
	}

	fmt.Println(ui.Success(fmt.Sprintf("Removed worktree: %s/%s", projectName, worktreeName)))

	return nil
}
