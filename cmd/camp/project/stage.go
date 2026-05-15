package project

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectStageCmd = &cobra.Command{
	Use:   "stage",
	Short: "Stage changes in a project submodule",
	Long: `Stage changes within a project submodule without committing.

Runs the same auto-staging logic as 'camp project commit' (including
stale lock file cleanup) but stops before creating a commit, so you can
use a different commit strategy.

Auto-detects the current project from your working directory,
or use --project to specify a project by name.

Examples:
  # From within a project directory
  cd projects/my-api
  camp project stage

  # Specify project by name
  camp project stage --project my-api`,
	RunE: runProjectStage,
}

var projectStageProject string

func init() {
	projectStageCmd.Flags().StringVarP(&projectStageProject, "project", "p", "", "Project name (auto-detected from cwd if not specified)")

	if err := projectStageCmd.RegisterFlagCompletionFunc("project", cmdutil.CompleteProjectName); err != nil {
		panic(err)
	}

	Cmd.AddCommand(projectStageCmd)
}

func runProjectStage(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	result, err := projectsvc.Resolve(ctx, campRoot, projectStageProject)
	if err != nil {
		var notFound *projectsvc.ProjectNotFoundError
		if errors.As(err, &notFound) {
			fmt.Println(ui.Dim("\n" + projectsvc.FormatProjectList(notFound.AvailableProjects())))
		}
		return err
	}

	resolvedPath := result.Path

	if err := result.RequireGit("git staging"); err != nil {
		return err
	}

	relPath := result.LogicalPath
	if relPath == "" {
		relPath, _ = filepath.Rel(campRoot, resolvedPath)
	}
	fmt.Printf("Project: %s\n", ui.Value(relPath))

	executor, err := git.NewExecutor(resolvedPath)
	if err != nil {
		return camperrors.Wrap(err, "failed to initialize git")
	}

	fmt.Println(ui.Info("Staging changes..."))
	if err := executor.StageAll(ctx); err != nil {
		return camperrors.Wrap(err, "failed to stage")
	}

	cmdutil.ShowStagedSummary(ctx, resolvedPath)

	hasChanges, err := executor.HasChanges(ctx)
	if err != nil {
		return err
	}
	if !hasChanges {
		fmt.Println(ui.Success("Nothing to stage in project"))
		return nil
	}

	fmt.Println(ui.Success("✓ Project changes staged"))
	fmt.Println(ui.Dim("Run 'git commit' inside the project or 'camp p commit --amend' to record them."))
	return nil
}
