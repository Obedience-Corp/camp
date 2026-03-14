package main

import (
	"context"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os/exec"
	"path/filepath"
	"strings"

	refspkg "github.com/Obedience-Corp/camp/cmd/camp/refs"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var refsSyncCmd = refspkg.Cmd

var refsSyncOpts struct {
	dryRun bool
	force  bool
}

func init() {
	refsSyncCmd.RunE = runRefsSync
	refsSyncCmd.Flags().BoolVarP(&refsSyncOpts.dryRun, "dry-run", "n", false, "Show plan without executing")
	refsSyncCmd.Flags().BoolVarP(&refsSyncOpts.force, "force", "f", false, "Skip safety checks (staged changes)")
}

type refChange struct {
	Path        string
	Name        string
	RecordedSHA string
	CurrentSHA  string
	Changed     bool
}

func runRefsSync(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	// Get submodule list
	paths, err := git.ListSubmodulePaths(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "listing submodules")
	}
	if len(args) > 0 {
		paths = filterRefPaths(paths, args)
	}
	if len(paths) == 0 {
		fmt.Println(ui.Info("No submodules found"))
		return nil
	}

	// Safety: check for existing staged changes
	if !refsSyncOpts.force {
		stagedCmd := exec.CommandContext(ctx, "git", "-C", campRoot, "diff", "--cached", "--quiet")
		if err := stagedCmd.Run(); err != nil {
			return fmt.Errorf("campaign root has staged changes; use --force to override")
		}
	}

	// Detect ref changes
	changes, err := detectRefChanges(ctx, campRoot, paths)
	if err != nil {
		return err
	}

	// Display plan
	displayRefPlan(changes)

	// Filter to only changed refs
	var toSync []string
	var names []string
	for _, c := range changes {
		if c.Changed {
			toSync = append(toSync, c.Path)
			names = append(names, c.Name)
		}
	}

	if len(toSync) == 0 {
		fmt.Println(ui.Success("All submodule refs are up to date"))
		return nil
	}

	if refsSyncOpts.dryRun {
		fmt.Println(ui.Info("Dry run — no changes made"))
		return nil
	}

	// Stage all changed refs
	executor, err := git.NewExecutor(campRoot)
	if err != nil {
		return camperrors.Wrap(err, "git executor")
	}
	if err := executor.Stage(ctx, toSync); err != nil {
		return camperrors.Wrap(err, "staging refs")
	}

	// Create atomic commit
	cfg, _ := config.LoadCampaignConfig(ctx, campRoot)
	msg := fmt.Sprintf("sync submodule refs: %s", strings.Join(names, ", "))
	if cfg != nil {
		msg = git.PrependCampaignTag(cfg.ID, msg)
	}
	if err := executor.Commit(ctx, &git.CommitOptions{Message: msg}); err != nil {
		return camperrors.Wrap(err, "commit")
	}

	fmt.Println(ui.Success(fmt.Sprintf("✓ Synced %d submodule ref(s)", len(toSync))))
	return nil
}

func detectRefChanges(ctx context.Context, campRoot string, paths []string) ([]refChange, error) {
	var changes []refChange
	for _, p := range paths {
		fullPath := filepath.Join(campRoot, p)

		// Get SHA recorded in campaign root's index
		lsTreeOut, err := exec.CommandContext(ctx, "git", "-C", campRoot,
			"ls-tree", "HEAD", "--", p).Output()
		if err != nil {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(string(lsTreeOut)))
		if len(fields) < 3 {
			continue
		}
		recordedSHA := fields[2]

		// Get submodule's current HEAD
		headOut, err := exec.CommandContext(ctx, "git", "-C", fullPath,
			"rev-parse", "HEAD").Output()
		if err != nil {
			continue
		}
		currentSHA := strings.TrimSpace(string(headOut))

		changes = append(changes, refChange{
			Path:        p,
			Name:        git.SubmoduleDisplayName(p),
			RecordedSHA: recordedSHA[:7],
			CurrentSHA:  currentSHA[:7],
			Changed:     recordedSHA != currentSHA,
		})
	}
	return changes, nil
}

func displayRefPlan(changes []refChange) {
	fmt.Println(ui.Header("Submodule ref sync plan"))
	for _, c := range changes {
		if c.Changed {
			fmt.Printf("  %-35s %s → %s\n", c.Path, ui.Dim(c.RecordedSHA), ui.Value(c.CurrentSHA))
		} else {
			fmt.Printf("  %-35s %s\n", c.Path, ui.Dim("(up to date)"))
		}
	}
	fmt.Println()
}

func filterRefPaths(all []string, targets []string) []string {
	targetSet := make(map[string]bool, len(targets))
	for _, t := range targets {
		targetSet[strings.TrimSuffix(t, "/")] = true
	}

	var filtered []string
	for _, p := range all {
		if targetSet[p] {
			filtered = append(filtered, p)
		}
	}
	return filtered
}
