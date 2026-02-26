package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
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
		return fmt.Errorf("failed to resolve target: %w", err)
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

		if isRebaseInProgress(ctx, target.Path) {
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

// pullTarget holds information about a repo to potentially pull.
type pullTarget struct {
	name   string
	path   string
	branch string
	isRoot bool // campaign root repo (skip recursive submodule fetch)
}

// runPullAll discovers all submodules + campaign root, and pulls them.
// When useDefault is true, submodules on detached HEAD or without upstream
// are switched to their remote's default branch before pulling.
func runPullAll(ctx context.Context, campRoot string, gitArgs []string, noRecurse, useDefault bool) error {
	green := lipgloss.NewStyle().Foreground(ui.SuccessColor)
	yellow := lipgloss.NewStyle().Foreground(ui.WarningColor)
	red := lipgloss.NewStyle().Foreground(ui.ErrorColor)
	dim := lipgloss.NewStyle().Foreground(ui.DimColor)

	fmt.Println(ui.Info("Pulling all repos..."))
	fmt.Println()

	// Discover submodules (including nested monorepo submodules)
	var (
		paths []string
		err   error
	)
	if noRecurse {
		paths, err = git.ListSubmodulePathsFiltered(ctx, campRoot, "projects/")
	} else {
		paths, err = git.ListSubmodulePathsRecursive(ctx, campRoot, "projects/")
	}
	if err != nil {
		return fmt.Errorf("failed to list submodules: %w", err)
	}

	// Build target list: campaign root first, then submodules
	targets := make([]pullTarget, 0, len(paths)+1)
	targets = append(targets, pullTarget{
		name:   "campaign root",
		path:   campRoot,
		isRoot: true,
	})
	for _, p := range paths {
		fullPath := filepath.Join(campRoot, p)
		branch, _ := gitOutput(ctx, fullPath, "rev-parse", "--abbrev-ref", "HEAD")
		targets = append(targets, pullTarget{
			name:   git.SubmoduleDisplayName(p),
			path:   fullPath,
			branch: branch,
		})
	}

	// Get campaign root branch
	targets[0].branch, _ = gitOutput(ctx, campRoot, "rev-parse", "--abbrev-ref", "HEAD")

	// Pull each target
	var pulled, skipped, failed int
	var errors []string

	for i := range targets {
		t := &targets[i]
		if ctx.Err() != nil {
			return ctx.Err()
		}

		originalBranch := t.branch

		// Skip detached HEAD (or checkout default branch if --default-branch)
		if t.branch == "" || t.branch == "HEAD" {
			if useDefault && !t.isRoot {
				branch, err := git.DetectDefaultBranch(ctx, t.path)
				if err != nil {
					fmt.Printf("  %-30s %s\n", t.name, yellow.Render("detached HEAD (no default found)"))
					skipped++
					continue
				}
				cmd := exec.CommandContext(ctx, "git", "-C", t.path, "checkout", branch)
				if out, err := cmd.CombinedOutput(); err != nil {
					fmt.Printf("  %-30s %s\n", t.name, red.Render(fmt.Sprintf("checkout %s failed", branch)))
					errors = append(errors, fmt.Sprintf("  %s: checkout %s: %s", t.name, branch, strings.TrimSpace(string(out))))
					failed++
					continue
				}
				t.branch = branch
			} else {
				fmt.Printf("  %-30s %s\n", t.name, yellow.Render("detached HEAD"))
				skipped++
				continue
			}
		}

		// Skip repos with no upstream tracking (or checkout default branch if --default-branch)
		if _, err := gitOutput(ctx, t.path, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
			if useDefault && !t.isRoot {
				branch, err := git.DetectDefaultBranch(ctx, t.path)
				if err != nil {
					fmt.Printf("  %-30s %s\n", t.name, yellow.Render("no upstream (no default found)"))
					skipped++
					continue
				}
				if t.branch != branch {
					cmd := exec.CommandContext(ctx, "git", "-C", t.path, "checkout", branch)
					if out, err := cmd.CombinedOutput(); err != nil {
						fmt.Printf("  %-30s %s\n", t.name, red.Render(fmt.Sprintf("checkout %s failed", branch)))
						errors = append(errors, fmt.Sprintf("  %s: checkout %s: %s", t.name, branch, strings.TrimSpace(string(out))))
						failed++
						continue
					}
					t.branch = branch
				}
				// Re-check upstream after checkout; skip if still missing
				if _, err := gitOutput(ctx, t.path, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
					fmt.Printf("  %-30s %s\n", t.name, yellow.Render("no upstream"))
					skipped++
					continue
				}
			} else {
				fmt.Printf("  %-30s %s\n", t.name, yellow.Render("no upstream"))
				skipped++
				continue
			}
		}

		if originalBranch != "" && originalBranch != "HEAD" && originalBranch != t.branch {
			fmt.Printf("  %-30s %s  %s  pulling... ",
				t.name, dim.Render(t.branch),
				dim.Render(fmt.Sprintf("(was %s)", originalBranch)))
		} else {
			fmt.Printf("  %-30s %s  pulling... ",
				t.name, dim.Render(t.branch))
		}

		pullArgs := []string{"-C", t.path, "pull"}
		if t.isRoot {
			// Campaign root: skip recursive submodule fetch since we pull
			// each submodule individually. Prevents failures from stale
			// submodule refs that no longer exist on their remotes.
			pullArgs = append(pullArgs, "--no-recurse-submodules")
		}
		pullArgs = append(pullArgs, gitArgs...)
		gitCmd := exec.CommandContext(ctx, "git", pullArgs...)
		output, err := gitCmd.CombinedOutput()
		if err != nil {
			// If a rebase failed mid-way, abort it so the repo isn't left
			// in a broken state. We can't stop to resolve during a batch.
			if isRebaseInProgress(ctx, t.path) {
				_ = abortRebase(ctx, t.path)
				fmt.Println(red.Render("conflict (aborted rebase)"))
				errors = append(errors, fmt.Sprintf("  %s: rebase conflict (try: camp pull -p %s --no-rebase)", t.name, t.name))
				failed++
				continue
			}

			fmt.Println(red.Render("failed"))
			errMsg := strings.TrimSpace(string(output))
			if isDivergentError(errMsg) {
				errMsg = "branches diverged (try: camp pull all --ff-only, --rebase, or resolve manually)"
			} else if errMsg == "" {
				errMsg = err.Error()
			}
			errors = append(errors, fmt.Sprintf("  %s: %s", t.name, errMsg))
			failed++
			continue
		}

		// Check if anything was actually pulled
		outStr := strings.TrimSpace(string(output))
		if strings.Contains(outStr, "Already up to date") {
			fmt.Println(dim.Render("up-to-date"))
			skipped++
		} else {
			fmt.Println(green.Render("done"))
			pulled++
		}
	}

	// Post-pull: report submodules with changed refs in the parent
	changedRefs := reportChangedRefs(ctx, campRoot, paths, yellow)

	// Summary
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

	if failed > 0 {
		return fmt.Errorf("%d repo(s) failed to pull", failed)
	}
	return nil
}

// reportChangedRefs checks which submodules have new refs after pulling
// and prints a summary. Does not stage or commit anything.
func reportChangedRefs(ctx context.Context, campRoot string, subPaths []string, style lipgloss.Style) int {
	var changed []string
	for _, p := range subPaths {
		fullPath := filepath.Join(campRoot, p)
		if checkParentNeedsCommit(ctx, campRoot, fullPath) {
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

// isRebaseInProgress checks whether a git rebase is in progress for the repo
// at the given path by looking for rebase-merge or rebase-apply directories.
func isRebaseInProgress(ctx context.Context, repoPath string) bool {
	gitDir, err := gitOutput(ctx, repoPath, "rev-parse", "--git-dir")
	if err != nil {
		return false
	}

	// Make gitDir absolute relative to repoPath if it's relative.
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoPath, gitDir)
	}

	for _, dir := range []string{"rebase-merge", "rebase-apply"} {
		if info, err := os.Stat(filepath.Join(gitDir, dir)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// abortRebase runs git rebase --abort for the repo at the given path.
func abortRebase(ctx context.Context, repoPath string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rebase", "--abort")
	return cmd.Run()
}
