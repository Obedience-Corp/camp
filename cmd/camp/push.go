package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/git"
	"github.com/obediencecorp/camp/internal/ui"
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
		return fmt.Errorf("failed to resolve target: %w", err)
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
	name  string
	path  string
	ahead int
}

// runPushAll discovers all submodules + campaign root, checks which have
// unpushed commits, and pushes them.
func runPushAll(ctx context.Context, campRoot string, gitArgs []string) error {
	green := lipgloss.NewStyle().Foreground(ui.SuccessColor)
	yellow := lipgloss.NewStyle().Foreground(ui.WarningColor)
	red := lipgloss.NewStyle().Foreground(ui.ErrorColor)
	dim := lipgloss.NewStyle().Foreground(ui.DimColor)

	fmt.Println(ui.Info("Pushing all repos with unpushed changes..."))
	fmt.Println()

	// Discover submodules
	paths, err := git.ListSubmodulePathsFiltered(ctx, campRoot, "projects/")
	if err != nil {
		return fmt.Errorf("failed to list submodules: %w", err)
	}

	// Build target list: submodules + campaign root
	targets := make([]pushTarget, 0, len(paths)+1)
	for _, p := range paths {
		targets = append(targets, pushTarget{
			name: filepath.Base(p),
			path: filepath.Join(campRoot, p),
		})
	}
	targets = append(targets, pushTarget{
		name: "campaign root",
		path: campRoot,
	})

	// Check ahead counts for each target
	for i := range targets {
		ahead := getAheadCount(ctx, targets[i].path)
		targets[i].ahead = ahead
	}

	// Push repos that are ahead
	var pushed, skipped, failed int
	var errors []string

	for _, t := range targets {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if t.ahead <= 0 {
			fmt.Printf("  %-20s %s\n", t.name, dim.Render("synced"))
			skipped++
			continue
		}

		fmt.Printf("  %-20s %s  pushing... ",
			t.name, yellow.Render(fmt.Sprintf("↑%d", t.ahead)))

		pushArgs := append([]string{"-C", t.path, "push"}, gitArgs...)
		gitCmd := exec.CommandContext(ctx, "git", pushArgs...)
		output, err := gitCmd.CombinedOutput()
		if err != nil {
			fmt.Println(red.Render("failed"))
			errMsg := strings.TrimSpace(string(output))
			if errMsg == "" {
				errMsg = err.Error()
			}
			errors = append(errors, fmt.Sprintf("  %s: %s", t.name, errMsg))
			failed++
			continue
		}

		fmt.Println(green.Render("done"))
		pushed++
	}

	// Summary
	fmt.Println()
	total := pushed + failed
	if total == 0 {
		fmt.Println(ui.Info("All repos are synced, nothing to push"))
	} else if failed == 0 {
		fmt.Println(green.Render(fmt.Sprintf("Pushed %d/%d repos successfully", pushed, total)))
	} else {
		fmt.Println(yellow.Render(fmt.Sprintf("Pushed %d/%d repos (%d failed)", pushed, total, failed)))
		for _, e := range errors {
			fmt.Println(red.Render(e))
		}
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
