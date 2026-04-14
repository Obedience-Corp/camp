package worktree

import (
	"errors"
	"fmt"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	intworktree "github.com/Obedience-Corp/camp/internal/worktree"
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
	Cmd.AddCommand(projectWorktreeRemoveCmd)
	projectWorktreeRemoveCmd.Flags().StringVarP(&wtRemoveProject, "project", "p", "", "Project name (auto-detected from cwd if not specified)")
	projectWorktreeRemoveCmd.Flags().BoolVarP(&wtRemoveForce, "force", "f", false, "Force removal even with uncommitted changes")

	if err := projectWorktreeRemoveCmd.RegisterFlagCompletionFunc("project", cmdutil.CompleteProjectName); err != nil {
		panic(err)
	}
}

func runProjectWorktreeRemove(cmd *cobra.Command, args []string) error {
	worktreeName := args[0]
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "failed to load campaign config")
	}

	resolved, err := project.Resolve(ctx, campRoot, wtRemoveProject)
	if err != nil {
		var notFound *project.ProjectNotFoundError
		if errors.As(err, &notFound) {
			fmt.Println(ui.Dim("\n" + project.FormatProjectList(notFound.AvailableProjects())))
		}
		return err
	}
	projectName := resolved.Name
	if err := resolved.RequireGit("git worktrees"); err != nil {
		return err
	}

	resolver := paths.NewResolver(campRoot, cfg.Paths())
	pathManager := intworktree.NewPathManager(resolver)
	if !pathManager.WorktreeExists(projectName, worktreeName) {
		return fmt.Errorf("worktree '%s' does not exist for project '%s'", worktreeName, projectName)
	}

	wtPath := pathManager.WorktreePath(projectName, worktreeName)
	git := intworktree.NewGitWorktree(resolved.Path)
	if err := git.Remove(ctx, wtPath, wtRemoveForce); err != nil {
		return camperrors.Wrap(err, "failed to remove worktree")
	}

	fmt.Println(ui.Success(fmt.Sprintf("Removed worktree: %s/%s", projectName, worktreeName)))
	return nil
}
