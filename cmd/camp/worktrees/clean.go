package worktrees

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	git "github.com/Obedience-Corp/camp/internal/git"
	navtui "github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	cleanProject      string
	cleanAll          bool
	cleanDryRun       bool
	cleanForce        bool
	cleanYes          bool
	cleanDiscardDirty bool
)

var worktreesCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove stale worktrees",
	Long: `Remove stale or orphaned worktrees.

Enumeration uses git worktree list as the source of truth (same as
camp worktrees list), then scans the preferred projects/worktrees/<project>/
layout for orphan directories not registered with git.

Auto-removal is conservative. Only worktrees whose .git file points at a
gitdir that no longer exists are removed from disk without further flags.
Full clones (.git is a directory) are listed and skipped for manual review.

Git-listed entries whose checkout directory (or .git file) is already gone
are not auto-pruned here; use git worktree prune in the project repo for
admin-only leftovers after the directory has vanished.

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
	worktreesCleanCmd.Flags().BoolVarP(&cleanYes, "yes", "y", false,
		"Skip confirmation prompt")
	worktreesCleanCmd.Flags().BoolVar(&cleanDiscardDirty, "discard-dirty", false,
		"Allow removal of worktrees with uncommitted changes (requires --force)")
}

type cleanResult struct {
	project     string
	worktree    string
	path        string
	reason      string
	gitDirEntry bool // true: .git is a directory; never auto-remove
	removed     bool
	removeErr   error
}

func runWorktreesClean(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if !cleanAll && cleanProject == "" {
		return camperrors.New("specify --all or --project <name>")
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

	// Find stale worktrees (git worktree list is the primary source of truth,
	// same as camp worktrees list; preferred-dir scan catches orphans only).
	stale, err := findStaleWorktrees(ctx, campRoot, pathManager, cleanProject)
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

	// Separate entries into removable and non-removable categories.
	var toRemove, toSkip []cleanResult
	for _, s := range stale {
		if s.gitDirEntry {
			toSkip = append(toSkip, s)
		} else {
			toRemove = append(toRemove, s)
		}
	}

	// Always print skipped entries with guidance.
	if len(toSkip) > 0 {
		fmt.Printf("\nSkipping %d entries whose .git is a directory (not a worktree):\n", len(toSkip))
		for _, s := range toSkip {
			fmt.Printf("  %s/%s\n", s.project, s.worktree)
			fmt.Printf("    Path:   %s\n", ui.Dim(s.path))
			fmt.Printf("    Reason: %s. This looks like a full clone;\n", ui.Dim(s.reason))
			fmt.Printf("            remove it manually after verifying no uncommitted work.\n\n")
		}
	}

	if len(toRemove) == 0 {
		fmt.Println(ui.Info("No removable stale worktrees found"))
		return nil
	}

	if cleanDryRun {
		fmt.Println(ui.Info("Dry run - no changes made"))
		return nil
	}

	// Require confirmation when stdin is a terminal and no --yes/--force.
	if !cleanYes && !cleanForce {
		if navtui.IsTerminal() {
			fmt.Printf("\nAbout to remove %d worktree director(ies). Proceed? [y/N] ", len(toRemove))
			var answer string
			_, _ = fmt.Scanln(&answer) //nolint:errcheck
			if !strings.HasPrefix(strings.ToLower(answer), "y") {
				fmt.Println(ui.Info("Aborted"))
				return nil
			}
		} else {
			return camperrors.New("refusing to delete worktrees without confirmation in non-interactive mode; pass --yes or --force")
		}
	}

	// Clean up (only the toRemove ones; toSkip already printed)
	fmt.Println(ui.Info("Cleaning up..."))
	var removed, failed int

	for i := range toRemove {
		err := cleanWorktree(ctx, cfg, resolver, &toRemove[i], cleanForce, cleanDiscardDirty)
		if err != nil {
			toRemove[i].removeErr = err
			if strings.Contains(err.Error(), "not found in campaign config") {
				// Non-fatal for skip cases (e.g. non-removable stale with unregistered project in test setups)
				fmt.Printf("  %s/%s: %s\n",
					toRemove[i].project, toRemove[i].worktree,
					ui.Dim("skipped (project not registered)"))
			} else {
				failed++
				fmt.Printf("  %s/%s: %s\n",
					toRemove[i].project, toRemove[i].worktree,
					ui.Error(err.Error()))
			}
		} else {
			toRemove[i].removed = true
			removed++
			fmt.Printf("  %s/%s: %s\n",
				toRemove[i].project, toRemove[i].worktree,
				ui.Success("removed"))
		}
	}

	fmt.Println()
	if failed > 0 {
		fmt.Printf("Removed %d, failed %d\n", removed, failed)
		return camperrors.New("some worktrees could not be removed")
	}

	fmt.Println(ui.Success(fmt.Sprintf("Removed %d stale worktrees", removed)))
	return nil
}

func findStaleWorktrees(ctx context.Context, campRoot string, pm *worktree.PathManager, filterProject string) ([]cleanResult, error) {
	var stale []cleanResult
	seen := make(map[string]struct{})

	// Project targets: shared with list so both commands scan the same set.
	targets, err := worktreeProjectTargets(ctx, campRoot, filterProject)
	if err != nil {
		return nil, err
	}

	for _, target := range targets {
		// Primary: every linked worktree git knows about, wherever it lives.
		gitEntries, listErr := worktree.NewGitWorktree(target.path).List(ctx)
		if listErr != nil {
			if filterProject != "" {
				return nil, camperrors.Wrapf(listErr, "failed to list git worktrees for project %q", target.name)
			}
			// Best-effort when cleaning --all: skip projects that fail list.
		} else {
			for _, entry := range gitEntries {
				if !worktree.IsLinkedWorktree(target.path, entry) {
					continue
				}
				cleanPath := filepath.Clean(entry.Path)
				if _, ok := seen[cleanPath]; ok {
					continue
				}
				seen[cleanPath] = struct{}{}
				if cr, ok := staleIfRemovable(target.name, filepath.Base(cleanPath), cleanPath); ok {
					stale = append(stale, cr)
				}
			}
		}

		// Secondary: preferred-location directories that may be orphaned (not in
		// git worktree list) but still leave broken worktree dirs on disk.
		fsNames, fsErr := pm.ListProjectWorktrees(target.name)
		if fsErr != nil {
			continue
		}
		for _, name := range fsNames {
			wtPath := filepath.Clean(pm.WorktreePath(target.name, name))
			if _, ok := seen[wtPath]; ok {
				continue
			}
			seen[wtPath] = struct{}{}
			if cr, ok := staleIfRemovable(target.name, name, wtPath); ok {
				stale = append(stale, cr)
			}
		}
	}

	return stale, nil
}

func staleIfRemovable(projectName, worktreeName, path string) (cleanResult, bool) {
	reason := checkWorktreeStale(path)
	if reason == "" {
		return cleanResult{}, false
	}
	if reason != "gitdir target does not exist" && !stalenessIsGitDir(reason) {
		return cleanResult{}, false
	}
	return cleanResult{
		project:     projectName,
		worktree:    worktreeName,
		path:        path,
		reason:      reason,
		gitDirEntry: stalenessIsGitDir(reason),
	}, true
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

// stalenessAllowsRemoval returns true only for the case where the gitdir
// target is definitively gone -- the worktree is unrecoverable by git and
// safe to remove from the filesystem without a git operation.
// Every other staleness reason requires a git operation (or is non-removable).
func stalenessAllowsRemoval(reason string) bool {
	return reason == "gitdir target does not exist"
}

// stalenessIsGitDir returns true when the entry appears to be a full clone
// (has a .git directory) rather than a worktree. These must never be auto-removed.
func stalenessIsGitDir(reason string) bool {
	return reason == ".git is a directory (not a worktree)"
}

func cleanWorktree(ctx context.Context, cfg *config.CampaignConfig, resolver *paths.Resolver, result *cleanResult, force, discardDirty bool) error {
	// Entries with a gitdir pointing nowhere are the only ones we remove
	// without a git operation. The git object store is already gone so there
	// is nothing for git to track.
	if stalenessAllowsRemoval(result.reason) {
		if err := os.RemoveAll(result.path); err != nil {
			return camperrors.Wrap(err, "failed to remove directory")
		}
		return nil
	}

	// For all other staleness reasons we need to route through git so that
	// git's own safety checks (dirty tree, active HEAD) are respected.
	var projectPath string
	for _, proj := range cfg.Projects {
		if proj.Name == result.project {
			projectPath = resolver.Project(result.project)
			break
		}
	}

	if projectPath == "" {
		return camperrors.Wrapf(camperrors.ErrNotFound,
			"project %q not found in campaign config; cannot remove worktree safely without a project path",
			result.project)
	}

	// When force is requested on a real worktree, run a dirty check before
	// passing --force to git. git worktree remove --force bypasses git's own
	// safety checks, so we apply the check ourselves.
	if force {
		hasChanges, err := git.HasChanges(ctx, result.path)
		if err == nil && hasChanges && !discardDirty {
			return camperrors.Wrapf(camperrors.ErrInvalidInput,
				"worktree %s/%s has uncommitted changes; commit or stash first, or pass --discard-dirty to override",
				result.project, result.worktree)
		}
	}

	gw := worktree.NewGitWorktree(projectPath)
	if err := gw.Remove(ctx, result.path, force); err != nil {
		return camperrors.Wrap(err, "git worktree remove failed")
	}
	return nil
}
