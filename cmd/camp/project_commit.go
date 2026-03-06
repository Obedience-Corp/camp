package main

import (
	"context"
	"errors"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os/exec"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit changes in a project submodule",
	Long: `Commit changes within a project submodule.

Auto-detects the current project from your working directory,
or use --project to specify a project by name.

Examples:
  # From within a project directory
  cd projects/my-api
  camp project commit -m "Fix bug"

  # Specify project by name
  camp project commit --project my-api -m "Update deps"`,
	RunE: runProjectCommit,
}

var (
	projectCommitProject string
	projectCommitMessage string
	projectCommitAll     bool
	projectCommitAmend   bool
	projectCommitSync    bool
)

func init() {
	projectCommitCmd.Flags().StringVarP(&projectCommitProject, "project", "p", "", "Project name (auto-detected from cwd if not specified)")
	projectCommitCmd.Flags().StringVarP(&projectCommitMessage, "message", "m", "", "Commit message (required)")
	projectCommitCmd.Flags().BoolVarP(&projectCommitAll, "all", "a", true, "Stage all changes")
	projectCommitCmd.Flags().BoolVar(&projectCommitAmend, "amend", false, "Amend the previous commit")
	projectCommitCmd.Flags().BoolVar(&projectCommitSync, "sync", false, "Sync submodule ref at campaign root after commit (opt-in)")

	projectCommitCmd.RegisterFlagCompletionFunc("project", completeProjectName)

	projectCmd.AddCommand(projectCommitCmd)
}

func runProjectCommit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Find campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	// Resolve project
	result, err := project.Resolve(ctx, campRoot, projectCommitProject)
	if err != nil {
		var notFound *project.ProjectNotFoundError
		if errors.As(err, &notFound) {
			fmt.Println(ui.Dim("\n" + project.FormatProjectList(notFound.AvailableProjects())))
		}
		return err
	}

	resolvedPath := result.Path

	// Display which project
	relPath, _ := filepath.Rel(campRoot, resolvedPath)
	fmt.Printf("Project: %s\n", ui.Value(relPath))

	// Create executor for the submodule
	executor, err := git.NewExecutor(resolvedPath)
	if err != nil {
		return camperrors.Wrap(err, "failed to initialize git")
	}

	// Get commit message - prompt if not provided
	message := projectCommitMessage
	if message == "" && !projectCommitAmend {
		var promptErr error
		message, promptErr = ui.PromptCommitMessageSimple(ctx, executor, false)
		if promptErr != nil {
			return camperrors.Wrap(promptErr, "prompt failed")
		}
		if message == "" {
			return fmt.Errorf("commit cancelled")
		}
	}

	// Stage if requested
	if projectCommitAll {
		fmt.Println(ui.Info("Staging changes..."))
		if err := executor.StageAll(ctx); err != nil {
			return camperrors.Wrap(err, "failed to stage")
		}
	}

	// Check for changes
	hasChanges, err := executor.HasChanges(ctx)
	if err != nil {
		return err
	}
	if !hasChanges && !projectCommitAmend {
		fmt.Println(ui.Success("Nothing to commit in project"))
		return nil
	}

	// Show what's staged
	showStagedSummary(ctx, resolvedPath)

	// Load campaign config (used for tag and parent sync)
	cfg, _ := config.LoadCampaignConfig(ctx, campRoot)

	// Prepend campaign tag (graceful degradation if config unavailable)
	if cfg != nil {
		message = git.PrependCampaignTag(cfg.ID, message)
	}

	// Commit
	fmt.Println(ui.Info("Committing changes..."))
	opts := &git.CommitOptions{
		Message: message,
		Amend:   projectCommitAmend,
	}
	if err := executor.Commit(ctx, opts); err != nil {
		if errors.Is(err, git.ErrNoChanges) {
			fmt.Println(ui.Success("Nothing to commit"))
			return nil
		}
		return camperrors.Wrap(err, "commit failed")
	}

	fmt.Println(ui.Success("✓ Project changes committed"))

	// Auto-sync submodule ref in campaign root
	if projectCommitSync && checkParentNeedsCommit(ctx, campRoot, resolvedPath) {
		if err := syncParentRef(ctx, campRoot, relPath, cfg); err != nil {
			fmt.Println()
			fmt.Println(ui.Warning("Could not auto-sync campaign root: " + err.Error()))
			fmt.Println(ui.Dim("Run 'camp commit' to update manually."))
		}
	}

	return nil
}

// syncParentRef stages and commits the submodule ref update in the campaign root.
func syncParentRef(ctx context.Context, campRoot, relPath string, cfg *config.CampaignConfig) error {
	parentExec, err := git.NewExecutor(campRoot)
	if err != nil {
		return camperrors.Wrap(err, "campaign root git")
	}

	if err := parentExec.Stage(ctx, []string{relPath}); err != nil {
		return camperrors.Wrap(err, "staging submodule ref")
	}

	projName := filepath.Base(relPath)
	msg := fmt.Sprintf("update %s submodule ref", projName)
	if cfg != nil {
		msg = git.PrependCampaignTag(cfg.ID, msg)
	}

	opts := &git.CommitOptions{Message: msg}
	if err := parentExec.Commit(ctx, opts); err != nil {
		if errors.Is(err, git.ErrNoChanges) {
			return nil
		}
		return camperrors.Wrap(err, "commit")
	}

	fmt.Println(ui.Success("✓ Campaign root synced (" + relPath + ")"))
	return nil
}

// checkParentNeedsCommit checks if the parent repo shows the submodule as modified.
func checkParentNeedsCommit(ctx context.Context, campRoot, projectPath string) bool {
	// Check if submodule is modified in parent
	cmd := exec.CommandContext(ctx, "git", "-C", campRoot,
		"diff", "--quiet", "--", projectPath)
	err := cmd.Run()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true // Submodule modified in parent
		}
	}

	return false
}
