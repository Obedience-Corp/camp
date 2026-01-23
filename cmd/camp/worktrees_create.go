package main

import (
	"context"
	"fmt"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/paths"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/obediencecorp/camp/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	createBranch    string
	createNewBranch bool
	createTrack     string
)

var worktreesCreateCmd = &cobra.Command{
	Use:   "create <project> <name>",
	Short: "Create a new worktree for a project",
	Long: `Create a new git worktree for a project in the standardized location.

The worktree will be created at: projects/worktrees/<project>/<name>/

Examples:
  # Create worktree on existing branch
  camp worktrees create my-api feature-auth

  # Create worktree with new branch
  camp worktrees create my-api experiment --new-branch

  # Create worktree tracking remote branch
  camp worktrees create web pr-review --track origin/feature-xyz`,
	Args: cobra.ExactArgs(2),
	RunE: runWorktreesCreate,
}

func init() {
	worktreesCmd.AddCommand(worktreesCreateCmd)

	worktreesCreateCmd.Flags().StringVarP(&createBranch, "branch", "b", "main",
		"Existing branch to checkout")
	worktreesCreateCmd.Flags().BoolVarP(&createNewBranch, "new-branch", "B", false,
		"Create new branch with worktree name")
	worktreesCreateCmd.Flags().StringVarP(&createTrack, "track", "t", "",
		"Remote branch to track (implies --new-branch)")
}

func runWorktreesCreate(cmd *cobra.Command, args []string) error {
	projectName := args[0]
	worktreeName := args[1]

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

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

	// Create resolver and creator
	resolver := paths.NewResolver(campRoot, cfg.Paths())
	creator := worktree.NewCreator(resolver, cfg)

	// Build options
	opts := &worktree.CreateOptions{
		Project:     projectName,
		Name:        worktreeName,
		Branch:      createBranch,
		NewBranch:   createNewBranch,
		TrackRemote: createTrack,
	}

	// Track implies new branch
	if opts.TrackRemote != "" {
		opts.NewBranch = true
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
