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

var pushCmd = &cobra.Command{
	Use:   "push [flags] [remote] [branch]",
	Short: "Push campaign changes to remote",
	Long: `Push campaign changes to the remote repository.

Works from anywhere within the campaign - always pushes from
the campaign root repository.

Use --sub to push from the submodule detected from your current directory.
Use --project/-p to push from a specific project.
Use 'camp push all' to push all repos that have unpushed commits.

Examples:
  camp push                    # Push current branch
  camp push origin main        # Push to specific remote/branch
  camp push --force            # Force push
  camp push -u origin feature  # Push and set upstream
  camp push --sub              # Push current submodule
  camp push -p projects/camp   # Push camp project
  camp push all                # Push all repos with unpushed commits
  camp push all --force        # Force push all repos`,
	RunE:               runPush,
	DisableFlagParsing: true,
}

func init() {
	rootCmd.AddCommand(pushCmd)
	pushCmd.GroupID = "git"
}

func runPush(cmd *cobra.Command, args []string) error {
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

	fullArgs := append([]string{"-C", target.Path, "push"}, gitArgs...)
	gitCmd := exec.CommandContext(ctx, "git", fullArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	gitCmd.Stdin = os.Stdin

	return gitCmd.Run()
}

// pushTarget holds information about a repo to potentially push.
type pushTarget struct {
	name string
	path string
}

type pushProbeStatus int

const (
	pushProbeReady pushProbeStatus = iota
	pushProbeSynced
	pushProbeNoUpstream
	pushProbeDetached
	pushProbeNoDestination
	pushProbeRejected
	pushProbeCheckFailed
)

type pushProbe struct {
	status pushProbeStatus
	detail string
	output string
}

// runPushAll discovers all submodules + campaign root, checks which have
// unpushed commits, and pushes them.
func runPushAll(ctx context.Context, campRoot string, gitArgs []string, noRecurse bool) error {
	green := lipgloss.NewStyle().Foreground(ui.SuccessColor)
	yellow := lipgloss.NewStyle().Foreground(ui.WarningColor)
	red := lipgloss.NewStyle().Foreground(ui.ErrorColor)
	dim := lipgloss.NewStyle().Foreground(ui.DimColor)

	fmt.Println(ui.Info("Pushing all repos with unpushed changes..."))
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
		return camperrors.Wrap(err, "failed to list submodules")
	}

	// Build target list: submodules + campaign root
	targets := make([]pushTarget, 0, len(paths)+1)
	for _, p := range paths {
		targets = append(targets, pushTarget{
			name: git.SubmoduleDisplayName(p),
			path: filepath.Join(campRoot, p),
		})
	}
	targets = append(targets, pushTarget{
		name: "campaign root",
		path: campRoot,
	})

	var pushed, synced, manual, failed int
	var errors []string

	for _, t := range targets {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		probe := probePushTarget(ctx, t.path, gitArgs)

		switch probe.status {
		case pushProbeSynced:
			fmt.Printf("  %-30s %s\n", t.name, dim.Render("synced"))
			synced++
			continue
		case pushProbeNoUpstream:
			fmt.Printf("  %-30s %s\n", t.name, yellow.Render("no upstream"))
			manual++
			continue
		case pushProbeDetached:
			fmt.Printf("  %-30s %s\n", t.name, yellow.Render("detached HEAD"))
			manual++
			continue
		case pushProbeNoDestination:
			fmt.Printf("  %-30s %s\n", t.name, yellow.Render("no push destination"))
			manual++
			continue
		case pushProbeRejected:
			fmt.Printf("  %-30s %s\n", t.name, red.Render("rejected"))
			errors = append(errors, fmt.Sprintf("  %s: %s", t.name, summarizePushOutput(probe.output)))
			failed++
			continue
		case pushProbeCheckFailed:
			fmt.Printf("  %-30s %s\n", t.name, red.Render("check failed"))
			errors = append(errors, fmt.Sprintf("  %s: %s", t.name, summarizePushOutput(probe.output)))
			failed++
			continue
		}

		detail := probe.detail
		if detail == "" {
			detail = "needs push"
		}
		fmt.Printf("  %-30s %s  pushing... ",
			t.name, yellow.Render(detail))

		pushArgs := append([]string{"-C", t.path, "push"}, gitArgs...)
		gitCmd := exec.CommandContext(ctx, "git", pushArgs...)
		output, err := gitCmd.CombinedOutput()
		if err != nil {
			fmt.Println(red.Render("failed"))
			errMsg := strings.TrimSpace(string(output))
			if errMsg == "" {
				errMsg = err.Error()
			}
			errors = append(errors, fmt.Sprintf("  %s: %s", t.name, summarizePushOutput(errMsg)))
			failed++
			continue
		}

		fmt.Println(green.Render("done"))
		pushed++
	}

	// Summary
	fmt.Println()
	switch {
	case pushed == 0 && failed == 0 && manual == 0:
		fmt.Println(ui.Info("All repos are synced, nothing to push"))
	case pushed == 0 && failed == 0:
		fmt.Println(yellow.Render("No repos were pushed"))
	case failed == 0:
		fmt.Println(green.Render(fmt.Sprintf("Pushed %d repo(s) successfully", pushed)))
	default:
		fmt.Println(yellow.Render(fmt.Sprintf("Pushed %d repo(s) (%d failed)", pushed, failed)))
		for _, e := range errors {
			fmt.Println(red.Render(e))
		}
	}
	if synced > 0 && (pushed > 0 || manual > 0 || failed > 0) {
		fmt.Println(dim.Render(fmt.Sprintf("%d repo(s) already synced", synced)))
	}
	if manual > 0 {
		fmt.Println(yellow.Render(fmt.Sprintf("%d repo(s) need manual attention", manual)))
	}

	if failed > 0 {
		return fmt.Errorf("%d repo(s) failed to push", failed)
	}
	return nil
}

// getAheadCount returns the number of commits ahead of upstream.
// Returns 0 if there's no upstream or on error.
func getAheadCount(ctx context.Context, repoPath string) int {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	parts := strings.Fields(strings.TrimSpace(string(output)))
	if len(parts) != 2 {
		return 0
	}

	var ahead int
	fmt.Sscanf(parts[0], "%d", &ahead)
	return ahead
}

func probePushTarget(ctx context.Context, repoPath string, gitArgs []string) pushProbe {
	args := append([]string{"-C", repoPath, "push", "--dry-run"}, gitArgs...)
	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(output))
	if err == nil {
		if isPushUpToDateOutput(out) {
			return pushProbe{status: pushProbeSynced}
		}

		return pushProbe{
			status: pushProbeReady,
			detail: describePendingPush(out, getAheadCount(ctx, repoPath)),
			output: out,
		}
	}

	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, "has no upstream branch"):
		return pushProbe{status: pushProbeNoUpstream, output: out}
	case strings.Contains(lower, "not currently on a branch"):
		return pushProbe{status: pushProbeDetached, output: out}
	case strings.Contains(lower, "no configured push destination"):
		return pushProbe{status: pushProbeNoDestination, output: out}
	case strings.Contains(lower, "[rejected]"), strings.Contains(lower, "failed to push some refs"):
		return pushProbe{status: pushProbeRejected, output: out}
	default:
		if out == "" {
			out = err.Error()
		}
		return pushProbe{status: pushProbeCheckFailed, output: out}
	}
}

func isPushUpToDateOutput(output string) bool {
	lower := strings.ToLower(strings.TrimSpace(output))
	return strings.Contains(lower, "everything up-to-date") ||
		strings.Contains(lower, "everything up to date") ||
		strings.Contains(lower, "[up to date]")
}

func describePendingPush(output string, ahead int) string {
	if ahead > 0 {
		return fmt.Sprintf("↑%d", ahead)
	}

	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "[new branch]"):
		return "new branch"
	case strings.Contains(lower, "forced update"):
		return "forced update"
	default:
		return "needs push"
	}
}

func summarizePushOutput(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "to ") || strings.HasPrefix(lower, "hint:") {
			continue
		}
		return line
	}

	return strings.TrimSpace(output)
}
