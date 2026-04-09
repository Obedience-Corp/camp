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
	wtAddProject    string
	wtAddBranch     string
	wtAddStartPoint string
	wtAddTrack      string
)

var projectWorktreeAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a new worktree for the project",
	Long: `Create a new git worktree for the current project.

Auto-detects the project from your current directory, or use --project
to specify explicitly.

The worktree will be created at: projects/worktrees/<project>/<name>/

By default, creates a new branch with the worktree name based on the current branch.
Use --branch to checkout an existing branch instead.

Examples:
  # Create worktree with new branch based on current branch (default)
  camp project worktree add feature-auth

  # Create worktree with new branch based on main
  camp project worktree add experiment --start-point main

  # Checkout existing branch (instead of creating new)
  camp project worktree add hotfix --branch hotfix-123

  # Track a remote branch
  camp project worktree add pr-review --track origin/feature-xyz

  # Explicit project
  camp project worktree add feature --project my-api`,
	Args: cobra.ExactArgs(1),
	RunE: runProjectWorktreeAdd,
}

func init() {
	Cmd.AddCommand(projectWorktreeAddCmd)

	projectWorktreeAddCmd.Flags().StringVarP(&wtAddProject, "project", "p", "", "Project name (auto-detected from cwd if not specified)")
	projectWorktreeAddCmd.Flags().StringVarP(&wtAddBranch, "branch", "b", "", "Checkout existing branch instead of creating new one")
	projectWorktreeAddCmd.Flags().StringVarP(&wtAddStartPoint, "start-point", "s", "", "Base branch/commit for new branch (default: current branch)")
	projectWorktreeAddCmd.Flags().StringVarP(&wtAddTrack, "track", "t", "", "Remote branch to track (creates new local tracking branch)")

	if err := projectWorktreeAddCmd.RegisterFlagCompletionFunc("project", cmdutil.CompleteProjectName); err != nil {
		panic(err)
	}
}

func runProjectWorktreeAdd(cmd *cobra.Command, args []string) error {
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

	resolved, err := project.Resolve(ctx, campRoot, wtAddProject)
	if err != nil {
		var notFound *project.ProjectNotFoundError
		if errors.As(err, &notFound) {
			fmt.Println(ui.Dim("\n" + project.FormatProjectList(notFound.AvailableProjects())))
		}
		return err
	}
	projectName := resolved.Name
	if resolved.Source == project.SourceLinkedNonGit {
		return fmt.Errorf("project %q is a linked non-git directory and does not support git worktrees", projectName)
	}

	resolver := paths.NewResolver(campRoot, cfg.Paths())
	creator := intworktree.NewCreator(resolver, cfg)
	opts := &intworktree.CreateOptions{
		Project:     projectName,
		ProjectPath: resolved.Path,
		Name:        worktreeName,
		TrackRemote: wtAddTrack,
	}

	if wtAddBranch != "" {
		opts.Branch = wtAddBranch
		opts.NewBranch = false
	} else if wtAddTrack != "" {
		opts.NewBranch = false
	} else {
		opts.NewBranch = true
		opts.Branch = worktreeName

		if wtAddStartPoint != "" {
			opts.StartPoint = wtAddStartPoint
		} else {
			git := intworktree.NewGitWorktree(resolved.Path)
			currentBranch, err := git.CurrentBranch(ctx)
			if err != nil {
				return camperrors.Wrap(err, "failed to detect current branch")
			}
			opts.StartPoint = currentBranch
		}
	}

	result, err := creator.Create(ctx, opts)
	if err != nil {
		return err
	}

	fmt.Println(ui.Success(fmt.Sprintf("Created worktree: %s/%s", result.Project, result.Name)))
	fmt.Printf("  Path:   %s\n", ui.Value(result.Path))
	fmt.Printf("  Branch: %s\n", ui.Value(result.Branch))
	fmt.Println()
	fmt.Println(ui.Dim(fmt.Sprintf("To navigate: cd %s", result.RelativePath)))

	return nil
}
