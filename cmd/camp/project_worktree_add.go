package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/paths"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/obediencecorp/camp/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	wtAddProject   string
	wtAddBranch    string
	wtAddNewBranch bool
	wtAddTrack     string
)

var projectWorktreeAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a new worktree for the project",
	Long: `Create a new git worktree for the current project.

Auto-detects the project from your current directory, or use --project
to specify explicitly.

The worktree will be created at: projects/worktrees/<project>/<name>/

Examples:
  # From within a project (auto-detect)
  cd projects/my-api
  camp project worktree add feature-auth

  # Create with new branch
  camp project worktree add experiment --new-branch

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
	projectWorktreeAddCmd.Flags().StringVarP(&wtAddBranch, "branch", "b", "main",
		"Existing branch to checkout")
	projectWorktreeAddCmd.Flags().BoolVarP(&wtAddNewBranch, "new-branch", "B", false,
		"Create new branch with worktree name")
	projectWorktreeAddCmd.Flags().StringVarP(&wtAddTrack, "track", "t", "",
		"Remote branch to track (implies --new-branch)")
}

func runProjectWorktreeAdd(cmd *cobra.Command, args []string) error {
	worktreeName := args[0]

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

	// Resolve project name
	projectName, err := resolveProjectName(ctx, campRoot, cfg, wtAddProject)
	if err != nil {
		showProjectList(ctx, campRoot)
		return err
	}

	// Create resolver and creator
	resolver := paths.NewResolver(campRoot, cfg.Paths())
	creator := worktree.NewCreator(resolver, cfg)

	// Build options
	opts := &worktree.CreateOptions{
		Project:     projectName,
		Name:        worktreeName,
		Branch:      wtAddBranch,
		NewBranch:   wtAddNewBranch,
		TrackRemote: wtAddTrack,
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

// resolveProjectName determines the project name from flag or current directory.
func resolveProjectName(ctx context.Context, campRoot string, cfg *config.CampaignConfig, flagProject string) (string, error) {
	if flagProject != "" {
		// Explicit project provided - validate it exists
		for _, proj := range cfg.Projects {
			if proj.Name == flagProject {
				return proj.Name, nil
			}
		}
		return "", fmt.Errorf("project '%s' not found in campaign", flagProject)
	}

	// Try to detect from current directory using existing logic
	resolvedPath, err := resolveProjectPath(ctx, campRoot, "")
	if err != nil {
		return "", err
	}

	// Extract project name from resolved path
	return projectNameFromPath(campRoot, cfg, resolvedPath)
}

// projectNameFromPath extracts the project name from a resolved project path.
func projectNameFromPath(campRoot string, cfg *config.CampaignConfig, absPath string) (string, error) {
	// First try to find in config
	for _, proj := range cfg.Projects {
		projPath := filepath.Join(campRoot, proj.Path)
		if projPath == absPath {
			return proj.Name, nil
		}
	}

	// Fall back to extracting from path structure (projects/<name>)
	projectsDir := filepath.Join(campRoot, "projects")
	if rel, err := filepath.Rel(projectsDir, absPath); err == nil {
		// rel should be the project name (first component)
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) > 0 && parts[0] != ".." && parts[0] != "." {
			return parts[0], nil
		}
	}

	return "", fmt.Errorf("could not determine project name for path: %s", absPath)
}
