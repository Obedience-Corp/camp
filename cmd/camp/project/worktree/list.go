package worktree

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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

	resolved, err := project.Resolve(ctx, campRoot, wtListProject)
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

	git := intworktree.NewGitWorktree(resolved.Path)
	entries, err := git.List(ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to list worktrees")
	}

	// git worktree list is the source of truth: it finds every linked
	// worktree regardless of where it lives on disk, not just those under
	// the conventional projects/worktrees/<project>/ layout.
	var linked []intworktree.GitWorktreeEntry
	for _, entry := range entries {
		if intworktree.IsLinkedWorktree(resolved.Path, entry) {
			linked = append(linked, entry)
		}
	}

	if len(linked) == 0 {
		fmt.Printf("No worktrees for project %s\n", ui.Value(projectName))
		fmt.Println(ui.Dim("Create one with: camp project worktree add <name>"))
		return nil
	}

	fmt.Printf("Worktrees for %s:\n\n", ui.Value(projectName))

	for _, entry := range linked {
		clean := filepath.Clean(entry.Path)
		name := filepath.Base(clean)

		fmt.Printf("  %s\n", ui.Value(name))
		fmt.Printf("    Branch: %s\n", ui.Dim(entry.Branch))
		if entry.IsLocked {
			fmt.Printf("    Status: %s\n", ui.Warning("locked"))
		}
		fmt.Printf("    Path:   %s\n", ui.Dim(displayWorktreePath(campRoot, clean)))
		fmt.Println()
	}

	return nil
}

// displayWorktreePath renders path relative to campRoot when it lives inside
// the campaign, falling back to the absolute path for worktrees created
// outside the campaign root.
func displayWorktreePath(campRoot, path string) string {
	rel, err := filepath.Rel(campRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}
