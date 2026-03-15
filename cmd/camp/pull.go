package main

import (
	"context"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull [flags] [remote] [branch]",
	Short: "Pull latest changes from remote",
	Long: `Pull latest changes from the remote repository.

Works from anywhere within the campaign - always pulls to
the campaign root repository.

Use --sub to pull the submodule detected from your current directory.
Use --project/-p to pull a specific project.
Use 'camp pull all' to pull all repos with upstream tracking.

Any git pull flags are passed through (e.g. --rebase, --ff-only).

Examples:
  camp pull                    # Pull current branch (merge)
  camp pull --rebase           # Pull with rebase
  camp pull --ff-only          # Fast-forward only
  camp pull --sub              # Pull current submodule
  camp pull -p projects/camp   # Pull camp project
  camp pull all                # Pull all repos
  camp pull all --ff-only      # Pull all repos, fast-forward only`,
	RunE:               runPull,
	DisableFlagParsing: true,
}

func init() {
	rootCmd.AddCommand(pullCmd)
	pullCmd.GroupID = "git"
}

func runPull(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	// Extract camp-specific flags, pass rest to git
	gitArgs, sub, project := git.ExtractSubFlags(args)

	target, err := git.ResolveTarget(ctx, campRoot, sub, project)
	if err != nil {
		return camperrors.Wrap(err, "failed to resolve target")
	}

	if target.IsSubmodule {
		fmt.Fprintln(os.Stderr, ui.Info(fmt.Sprintf("Submodule: %s", target.Name)))
	}

	fullArgs := append([]string{"-C", target.Path, "pull"}, gitArgs...)
	gitCmd := exec.CommandContext(ctx, "git", fullArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	gitCmd.Stdin = os.Stdin

	if err := gitCmd.Run(); err != nil {
		// Suppress cobra's usage output — this isn't a usage error.
		cmd.SilenceUsage = true

		if git.IsRebaseInProgress(ctx, target.Path) {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, ui.Warning("Rebase conflict in "+target.Name))
			fmt.Fprintln(os.Stderr, "  To abort:        git rebase --abort")
			fmt.Fprintln(os.Stderr, "  To resolve:      fix conflicts, git add, git rebase --continue")
			fmt.Fprintln(os.Stderr, "  To retry merge:  camp pull --no-rebase")
		}

		return err
	}

	return nil
}

// PullAllOptions holds configuration for pull-all operations.
type PullAllOptions struct {
	NoRecurse     bool
	DefaultBranch bool
}

// pullTarget holds information about a repo to potentially pull.
type pullTarget struct {
	name   string
	path   string
	branch string
	isRoot bool // campaign root repo (skip recursive submodule fetch)
}

// checkoutDefaultIfNeeded switches a submodule to its default branch when
// --default-branch is active and the submodule is on detached HEAD or has no
// upstream tracking. Returns the original branch name before any switch.
// If the checkout fails, it appends to errs and returns skip=true.
func checkoutDefaultIfNeeded(ctx context.Context, t *pullTarget) (originalBranch string, switched bool, err error) {
	originalBranch = t.branch

	if t.branch != "" && t.branch != "HEAD" {
		return originalBranch, false, nil
	}

	branch, err := git.CheckoutDefaultBranch(ctx, t.path)
	if err != nil {
		return originalBranch, false, err
	}
	t.branch = branch
	return originalBranch, true, nil
}

// runPullAll discovers all submodules + campaign root, and pulls them.
func runPullAll(ctx context.Context, campRoot string, gitArgs []string, opts PullAllOptions) error {
	green := lipgloss.NewStyle().Foreground(ui.SuccessColor)
	yellow := lipgloss.NewStyle().Foreground(ui.WarningColor)
	red := lipgloss.NewStyle().Foreground(ui.ErrorColor)
	dim := lipgloss.NewStyle().Foreground(ui.DimColor)

	fmt.Println(ui.Info("Pulling all repos..."))
	fmt.Println()

	paths, err := discoverSubmodules(ctx, campRoot, opts.NoRecurse)
	if err != nil {
		return err
	}

	targets := buildPullTargets(ctx, campRoot, paths)

	var pulled, skipped, failed int
	var errors []string

	for i := range targets {
		t := &targets[i]
		if ctx.Err() != nil {
			return ctx.Err()
		}

		result := pullSingleTarget(ctx, t, gitArgs, opts, dim, green, yellow, red)
		switch result.outcome {
		case pullOutcomePulled:
			pulled++
		case pullOutcomeSkipped:
			skipped++
		case pullOutcomeFailed:
			failed++
			errors = append(errors, result.errMsg)
		}
	}

	changedRefs := reportChangedRefs(ctx, campRoot, paths, yellow)
	printPullSummary(pulled, failed, changedRefs, errors, green, yellow, red, dim)

	if failed > 0 {
		return fmt.Errorf("%d repo(s) failed to pull", failed)
	}
	return nil
}

type pullOutcome int

const (
	pullOutcomePulled pullOutcome = iota
	pullOutcomeSkipped
	pullOutcomeFailed
)

type pullResult struct {
	outcome pullOutcome
	errMsg  string
}

// pullSingleTarget handles the checkout-if-needed, upstream check, and pull
// for a single target. Returns the outcome and optional error message.
func pullSingleTarget(
	ctx context.Context,
	t *pullTarget,
	gitArgs []string,
	opts PullAllOptions,
	dim, green, yellow, red lipgloss.Style,
) pullResult {
	originalBranch := t.branch

	// Handle detached HEAD
	if t.branch == "" || t.branch == "HEAD" {
		if opts.DefaultBranch && !t.isRoot {
			if _, _, err := checkoutDefaultIfNeeded(ctx, t); err != nil {
				fmt.Printf("  %-30s %s\n", t.name, yellow.Render("detached HEAD (checkout failed)"))
				return pullResult{outcome: pullOutcomeSkipped}
			}
		} else {
			fmt.Printf("  %-30s %s\n", t.name, yellow.Render("detached HEAD"))
			return pullResult{outcome: pullOutcomeSkipped}
		}
	}

	// Check upstream tracking
	if _, err := git.Output(ctx, t.path, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
		if opts.DefaultBranch && !t.isRoot {
			branch, checkoutErr := git.CheckoutDefaultBranch(ctx, t.path)
			if checkoutErr != nil {
				fmt.Printf("  %-30s %s\n", t.name, yellow.Render("no upstream (checkout failed)"))
				return pullResult{outcome: pullOutcomeSkipped}
			}
			if t.branch != branch {
				t.branch = branch
			}
			// Re-check upstream after checkout
			if _, err := git.Output(ctx, t.path, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
				fmt.Printf("  %-30s %s\n", t.name, yellow.Render("no upstream"))
				return pullResult{outcome: pullOutcomeSkipped}
			}
		} else {
			fmt.Printf("  %-30s %s\n", t.name, yellow.Render("no upstream"))
			return pullResult{outcome: pullOutcomeSkipped}
		}
	}

	// Print progress line
	if originalBranch != "" && originalBranch != "HEAD" && originalBranch != t.branch {
		fmt.Printf("  %-30s %s  %s  pulling... ",
			t.name, dim.Render(t.branch),
			dim.Render(fmt.Sprintf("(was %s)", originalBranch)))
	} else {
		fmt.Printf("  %-30s %s  pulling... ",
			t.name, dim.Render(t.branch))
	}

	// Execute pull
	pullArgs := []string{"-C", t.path, "pull"}
	if t.isRoot {
		pullArgs = append(pullArgs, "--no-recurse-submodules")
	}
	pullArgs = append(pullArgs, gitArgs...)
	gitCmd := exec.CommandContext(ctx, "git", pullArgs...)
	output, err := gitCmd.CombinedOutput()
	if err != nil {
		return handlePullError(ctx, t, output, err, red)
	}

	outStr := strings.TrimSpace(string(output))
	if strings.Contains(outStr, "Already up to date") {
		fmt.Println(dim.Render("up-to-date"))
		return pullResult{outcome: pullOutcomeSkipped}
	}
	fmt.Println(green.Render("done"))
	return pullResult{outcome: pullOutcomePulled}
}

// handlePullError processes a failed git pull, aborting any in-progress rebase.
func handlePullError(ctx context.Context, t *pullTarget, output []byte, err error, red lipgloss.Style) pullResult {
	if git.IsRebaseInProgress(ctx, t.path) {
		_ = abortRebase(ctx, t.path)
		fmt.Println(red.Render("conflict (aborted rebase)"))
		return pullResult{
			outcome: pullOutcomeFailed,
			errMsg:  fmt.Sprintf("  %s: rebase conflict (try: camp pull -p %s --no-rebase)", t.name, t.name),
		}
	}

	fmt.Println(red.Render("failed"))
	errMsg := strings.TrimSpace(string(output))
	if isDivergentError(errMsg) {
		errMsg = "branches diverged (try: camp pull all --ff-only, --rebase, or resolve manually)"
	} else if errMsg == "" {
		errMsg = err.Error()
	}
	return pullResult{
		outcome: pullOutcomeFailed,
		errMsg:  fmt.Sprintf("  %s: %s", t.name, errMsg),
	}
}

// discoverSubmodules lists submodule paths, optionally filtering to top-level only.
func discoverSubmodules(ctx context.Context, campRoot string, noRecurse bool) ([]string, error) {
	if noRecurse {
		paths, err := git.ListSubmodulePathsFiltered(ctx, campRoot, "projects/")
		if err != nil {
			return nil, camperrors.Wrap(err, "failed to list submodules")
		}
		return paths, nil
	}
	paths, err := git.ListSubmodulePathsRecursive(ctx, campRoot, "projects/")
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to list submodules")
	}
	return paths, nil
}

// buildPullTargets constructs the target list: campaign root first, then submodules.
func buildPullTargets(ctx context.Context, campRoot string, paths []string) []pullTarget {
	targets := make([]pullTarget, 0, len(paths)+1)
	rootBranch, _ := git.Output(ctx, campRoot, "rev-parse", "--abbrev-ref", "HEAD")
	targets = append(targets, pullTarget{
		name:   "campaign root",
		path:   campRoot,
		branch: rootBranch,
		isRoot: true,
	})
	for _, p := range paths {
		fullPath := filepath.Join(campRoot, p)
		branch, _ := git.Output(ctx, fullPath, "rev-parse", "--abbrev-ref", "HEAD")
		targets = append(targets, pullTarget{
			name:   git.SubmoduleDisplayName(p),
			path:   fullPath,
			branch: branch,
		})
	}
	return targets
}

// printPullSummary outputs the final pull-all summary.
func printPullSummary(pulled, failed, changedRefs int, errors []string, green, yellow, red, dim lipgloss.Style) {
	fmt.Println()
	total := pulled + failed
	if total == 0 {
		fmt.Println(ui.Info("All repos are up-to-date, nothing to pull"))
	} else if failed == 0 {
		fmt.Println(green.Render(fmt.Sprintf("Pulled %d/%d repos successfully", pulled, total)))
	} else {
		fmt.Println(yellow.Render(fmt.Sprintf("Pulled %d/%d repos (%d failed)", pulled, total, failed)))
		for _, e := range errors {
			fmt.Println(red.Render(e))
		}
	}

	if changedRefs > 0 {
		fmt.Println()
		fmt.Println(dim.Render("  Run 'camp commit' to record these ref updates."))
	}
}

// reportChangedRefs checks which submodules have new refs after pulling
// and prints a summary. Does not stage or commit anything.
func reportChangedRefs(ctx context.Context, campRoot string, subPaths []string, style lipgloss.Style) int {
	var changed []string
	for _, p := range subPaths {
		fullPath := filepath.Join(campRoot, p)
		if git.HasPathDiff(ctx, campRoot, fullPath) {
			changed = append(changed, p)
		}
	}

	if len(changed) == 0 {
		return 0
	}

	fmt.Println()
	fmt.Println(style.Render("  Submodule refs updated (not yet committed):"))
	for _, p := range changed {
		fmt.Printf("    %-30s (new commits)\n", git.SubmoduleDisplayName(p))
	}

	return len(changed)
}

// isDivergentError checks if git output indicates divergent branches.
func isDivergentError(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "divergent branches") ||
		strings.Contains(lower, "need to specify how to reconcile")
}

// abortRebase runs git rebase --abort for the repo at the given path.
func abortRebase(ctx context.Context, repoPath string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rebase", "--abort")
	return cmd.Run()
}
