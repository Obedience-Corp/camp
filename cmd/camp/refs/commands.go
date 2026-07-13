package refs

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var refsSyncOpts struct {
	dryRun bool
	force  bool
}

type refChange struct {
	Path        string
	Name        string
	RecordedSHA string
	CurrentSHA  string
	Changed     bool
}

type refSkip struct {
	Path   string
	Reason string
}

func init() {
	Cmd.RunE = runRefsSync
	Cmd.Flags().BoolVarP(&refsSyncOpts.dryRun, "dry-run", "n", false, "Show plan without executing")
	Cmd.Flags().BoolVarP(&refsSyncOpts.force, "force", "f", false, "Skip safety checks (staged changes)")
}

func runRefsSync(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

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

	if !refsSyncOpts.force {
		stagedCmd := exec.CommandContext(ctx, "git", "-C", campRoot, "diff", "--cached", "--quiet")
		if err := stagedCmd.Run(); err != nil {
			return camperrors.Newf("campaign root has staged changes; use --force to override")
		}
	}

	changes, skips, err := detectRefChanges(ctx, campRoot, paths)
	if err != nil {
		return err
	}

	displayRefPlan(changes)
	displayRefSkips(skips)

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

	cfg, _ := config.LoadCampaignConfig(ctx, campRoot)
	msg := fmt.Sprintf("sync submodule refs: %s", strings.Join(names, ", "))
	if cfg != nil && config.EffectiveCommitPrefs(ctx, campRoot).TagCommits() {
		msg = git.PrependContextTagsFull(cfg.Name, cfg.ID, "", "", "", msg)
	}
	if err := git.CommitScoped(ctx, campRoot, toSync, &git.CommitOptions{Message: msg}); err != nil {
		return camperrors.Wrap(err, "commit")
	}

	fmt.Println(ui.Success(fmt.Sprintf("✓ Synced %d submodule ref(s)", len(toSync))))
	return nil
}

func detectRefChanges(ctx context.Context, campRoot string, paths []string) ([]refChange, []refSkip, error) {
	var changes []refChange
	var skips []refSkip
	for _, p := range paths {
		fullPath := filepath.Join(campRoot, p)

		lsTreeOut, err := exec.CommandContext(ctx, "git", "-C", campRoot,
			"ls-tree", "HEAD", "--", p).Output()
		if err != nil {
			skips = append(skips, refSkip{Path: p, Reason: "could not read recorded ref: " + err.Error()})
			continue
		}
		fields := strings.Fields(strings.TrimSpace(string(lsTreeOut)))
		if len(fields) < 3 {
			skips = append(skips, refSkip{Path: p, Reason: "not recorded in HEAD"})
			continue
		}
		recordedSHA := fields[2]

		headOut, err := exec.CommandContext(ctx, "git", "-C", fullPath,
			"rev-parse", "HEAD").Output()
		if err != nil {
			skips = append(skips, refSkip{Path: p, Reason: "could not read submodule HEAD: " + err.Error()})
			continue
		}
		currentSHA := strings.TrimSpace(string(headOut))
		changed := recordedSHA != currentSHA

		changes = append(changes, refChange{
			Path:        p,
			Name:        git.SubmoduleDisplayName(p),
			RecordedSHA: shortSHA(recordedSHA),
			CurrentSHA:  shortSHA(currentSHA),
			Changed:     changed,
		})
		if !changed {
			skips = append(skips, refSkip{Path: p, Reason: "already up to date"})
		}
	}
	return changes, skips, nil
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

func displayRefSkips(skips []refSkip) {
	if len(skips) == 0 {
		return
	}

	fmt.Println(ui.Dim("Skipped submodules:"))
	for _, skip := range skips {
		fmt.Printf("  %-35s %s\n", skip.Path, ui.Dim(skip.Reason))
	}
	fmt.Println()
}

func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
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
