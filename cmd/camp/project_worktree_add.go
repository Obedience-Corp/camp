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
	projectWorktreeCmd.AddCommand(projectWorktreeAddCmd)

	projectWorktreeAddCmd.Flags().StringVarP(&wtAddProject, "project", "p", "",
		"Project name (auto-detected from cwd if not specified)")
	projectWorktreeAddCmd.Flags().StringVarP(&wtAddBranch, "branch", "b", "",
		"Checkout existing branch instead of creating new one")
	projectWorktreeAddCmd.Flags().StringVarP(&wtAddStartPoint, "start-point", "s", "",
		"Base branch/commit for new branch (default: current branch)")
	projectWorktreeAddCmd.Flags().StringVarP(&wtAddTrack, "track", "t", "",
		"Remote branch to track (creates new local tracking branch)")
}

func runProjectWorktreeAdd(cmd *cobra.Command, args []string) error {
	worktreeName := args[0]

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
	resolved, err := project.Resolve(ctx, campRoot, wtAddProject)
	if err != nil {
		var notFound *project.ProjectNotFoundError
		if errors.As(err, &notFound) {
			fmt.Println(ui.Dim("\n" + project.FormatProjectList(notFound.AvailableProjects())))
		}
		return err
	}
	projectName := resolved.Name

	// Create resolver and creator
	resolver := paths.NewResolver(campRoot, cfg.Paths())
	creator := worktree.NewCreator(resolver, cfg)

	// Build options based on new semantics:
	// - Default: create new branch with worktree name, based on current branch
	// - --branch: checkout existing branch
	// - --start-point: specify base for new branch
	// - --track: track remote branch
	opts := &worktree.CreateOptions{
		Project:     projectName,
		Name:        worktreeName,
		TrackRemote: wtAddTrack,
	}

	if wtAddBranch != "" {
		// Explicit existing branch requested
		opts.Branch = wtAddBranch
		opts.NewBranch = false
	} else if wtAddTrack != "" {
		// Track remote branch (handled by TrackRemote)
		opts.NewBranch = false
	} else {
		// Default: create new branch based on current branch
		opts.NewBranch = true
		opts.Branch = worktreeName

		// Determine start point
		if wtAddStartPoint != "" {
			opts.StartPoint = wtAddStartPoint
		} else {
			// Get current branch as default start point
			projectPath := resolver.Project(projectName)
			git := worktree.NewGitWorktree(projectPath)
			currentBranch, err := git.CurrentBranch(ctx)
			if err != nil {
				return fmt.Errorf("failed to detect current branch: %w", err)
			}
			opts.StartPoint = currentBranch
		}
	}

	// Execute creation
	result, err := creator.Create(ctx, opts)
	if err != nil {
		return err
	}

	// Success output
	fmt.Println(ui.Success(fmt.Sprintf("Created worktree: %s/%s", result.Project, result.Name)))
	fmt.Printf("  Path:   %s\n", ui.Value(result.Path))
	fmt.Printf("  Branch: %s\n", ui.Value(result.Branch))
	fmt.Println()
	fmt.Println(ui.Dim(fmt.Sprintf("To navigate: cd %s", result.RelativePath)))

	return nil
}

