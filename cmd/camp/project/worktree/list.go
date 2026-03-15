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
	Cmd.AddCommand(projectWorktreeListCmd)
	projectWorktreeListCmd.Flags().StringVarP(&wtListProject, "project", "p", "", "Project name (auto-detected from cwd if not specified)")

	if err := projectWorktreeListCmd.RegisterFlagCompletionFunc("project", cmdutil.CompleteProjectName); err != nil {
		panic(err)
	}
}

func runProjectWorktreeList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "failed to load campaign config")
	}

	resolved, err := project.Resolve(ctx, campRoot, wtListProject)
	if err != nil {
		var notFound *project.ProjectNotFoundError
		if errors.As(err, &notFound) {
			fmt.Println(ui.Dim("\n" + project.FormatProjectList(notFound.AvailableProjects())))
		}
		return err
	}
	projectName := resolved.Name

	resolver := paths.NewResolver(campRoot, cfg.Paths())
	pathManager := intworktree.NewPathManager(resolver)

	projectPath := resolver.Project(projectName)
	git := intworktree.NewGitWorktree(projectPath)
	entries, err := git.List(ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to list worktrees")
	}

	names, err := pathManager.ListProjectWorktrees(projectName)
	if err != nil {
		return camperrors.Wrap(err, "failed to list worktree directories")
	}

	if len(names) == 0 {
		fmt.Printf("No worktrees for project %s\n", ui.Value(projectName))
		fmt.Println(ui.Dim("Create one with: camp project worktree add <name>"))
		return nil
	}

	fmt.Printf("Worktrees for %s:\n\n", ui.Value(projectName))

	entryMap := make(map[string]intworktree.GitWorktreeEntry)
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
