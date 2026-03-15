package worktrees

import (
	"context"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	cleanProject string
	cleanAll     bool
	cleanDryRun  bool
	cleanForce   bool
)

var worktreesCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove stale worktrees",
	Long: `Remove stale or orphaned worktrees.

Stale worktrees are those where the git worktree reference is broken or
the directory no longer exists. This command cleans up both the filesystem
and git's internal worktree tracking.

Examples:
  # Preview what would be cleaned
  camp worktrees clean --dry-run

  # Clean all stale worktrees
  camp worktrees clean --all

  # Clean stale worktrees for a specific project
  camp worktrees clean --project my-api

  # Force remove even worktrees with changes
  camp worktrees clean --all --force`,
	RunE: runWorktreesClean,
}

func init() {
	Cmd.AddCommand(worktreesCleanCmd)

	worktreesCleanCmd.Flags().StringVarP(&cleanProject, "project", "p", "",
		"Clean worktrees for specific project only")
	worktreesCleanCmd.Flags().BoolVar(&cleanAll, "all", false,
		"Clean all stale worktrees")
	worktreesCleanCmd.Flags().BoolVarP(&cleanDryRun, "dry-run", "n", false,
		"Preview what would be removed")
	worktreesCleanCmd.Flags().BoolVarP(&cleanForce, "force", "f", false,
		"Force removal even with uncommitted changes")
}

type cleanResult struct {
	project   string
	worktree  string
	path      string
	reason    string
	removed   bool
	removeErr error
}

func runWorktreesClean(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if !cleanAll && cleanProject == "" {
		return fmt.Errorf("specify --all or --project <name>")
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "failed to load campaign config")
	}

	resolver := paths.NewResolver(campRoot, cfg.Paths())
	pathManager := worktree.NewPathManager(resolver)

	// Find stale worktrees
	stale, err := findStaleWorktrees(ctx, pathManager, cfg, cleanProject)
	if err != nil {
		return err
	}

	if len(stale) == 0 {
		fmt.Println(ui.Success("No stale worktrees found"))
		return nil
	}

	// Display what we found
	fmt.Printf("Found %d stale worktrees:\n\n", len(stale))
	for _, s := range stale {
		fmt.Printf("  %s/%s\n", s.project, s.worktree)
		fmt.Printf("    Path:   %s\n", ui.Dim(s.path))
		fmt.Printf("    Reason: %s\n", ui.Dim(s.reason))
		fmt.Println()
	}

	if cleanDryRun {
		fmt.Println(ui.Info("Dry run - no changes made"))
		return nil
	}

	// Clean up
	fmt.Println(ui.Info("Cleaning up..."))
	var removed, failed int

	for i := range stale {
		err := cleanWorktree(ctx, cfg, resolver, &stale[i], cleanForce)
		if err != nil {
			stale[i].removeErr = err
			failed++
			fmt.Printf("  %s/%s: %s\n",
				stale[i].project, stale[i].worktree,
				ui.Error(err.Error()))
		} else {
			stale[i].removed = true
			removed++
			fmt.Printf("  %s/%s: %s\n",
				stale[i].project, stale[i].worktree,
				ui.Success("removed"))
		}
	}

	fmt.Println()
	if failed > 0 {
		fmt.Printf("Removed %d, failed %d\n", removed, failed)
		return fmt.Errorf("some worktrees could not be removed")
	}

	fmt.Println(ui.Success(fmt.Sprintf("Removed %d stale worktrees", removed)))
	return nil
}

func findStaleWorktrees(ctx context.Context, pm *worktree.PathManager, cfg *config.CampaignConfig, filterProject string) ([]cleanResult, error) {
	var stale []cleanResult

	// Get projects to scan
	var projectNames []string
	if filterProject != "" {
		projectNames = []string{filterProject}
	} else {
		projects, err := pm.ListAllProjects()
		if err != nil {
			return nil, err
		}
		projectNames = projects
	}

	// Check each project's worktrees
	for _, projectName := range projectNames {
		fsWorktrees, err := pm.ListProjectWorktrees(projectName)
		if err != nil {
			continue
		}

		for _, name := range fsWorktrees {
			wtPath := pm.WorktreePath(projectName, name)
			reason := checkWorktreeStale(wtPath)
			if reason != "" {
				stale = append(stale, cleanResult{
					project:  projectName,
					worktree: name,
					path:     wtPath,
					reason:   reason,
				})
			}
		}
	}

	return stale, nil
}

func checkWorktreeStale(path string) string {
	gitPath := filepath.Join(path, ".git")

	info, err := os.Stat(gitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing .git file"
		}
		return fmt.Sprintf("cannot access .git: %v", err)
	}

	if info.IsDir() {
		return ".git is a directory (not a worktree)"
	}

	// Read .git file content
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return fmt.Sprintf("cannot read .git file: %v", err)
	}

	// Check gitdir path exists
	line := string(content)
	if len(line) < 8 || line[:8] != "gitdir: " {
		return "invalid .git file format"
	}

	gitdir := trimSpace(line[8:])
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(path, gitdir)
	}

	if _, err := os.Stat(gitdir); os.IsNotExist(err) {
		return "gitdir target does not exist"
	}

	return "" // Not stale
}

func cleanWorktree(ctx context.Context, cfg *config.CampaignConfig, resolver *paths.Resolver, result *cleanResult, force bool) error {
	// Get project path
	var projectPath string
	for _, proj := range cfg.Projects {
		if proj.Name == result.project {
			projectPath = resolver.Project(result.project)
			break
		}
	}

	if projectPath != "" {
		// Try to remove via git worktree remove
		git := worktree.NewGitWorktree(projectPath)
		if err := git.Remove(ctx, result.path, force); err != nil {
			// Fall through to filesystem removal
		}
	}

	// Remove the directory
	if err := os.RemoveAll(result.path); err != nil {
		return camperrors.Wrap(err, "failed to remove directory")
	}

	return nil
}
