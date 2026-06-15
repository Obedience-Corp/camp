package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	pullsvc "github.com/Obedience-Corp/camp/internal/pull"
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
Use --project to pull a specific project.
Use 'camp pull all' to pull all repos with upstream tracking.

Any git pull flags are passed through (e.g. --rebase, --ff-only).

Examples:
  camp pull                    # Pull current branch (merge)
  camp pull --rebase           # Pull with rebase
  camp pull --ff-only          # Fast-forward only
  camp pull --sub              # Pull current submodule
  camp pull --project=projects/camp  # Pull camp project
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

	if _, err := pullsvc.RunGitPullWithLockRetry(ctx, target.Path, gitArgs, true, pullsvc.DefaultIO()); err != nil {
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

func runPullAll(ctx context.Context, campRoot string, gitArgs []string, opts pullsvc.Options) error {
	_, err := pullsvc.RunAll(ctx, campRoot, gitArgs, opts, newPullHooks())
	return err
}

type pullStyles struct {
	green  lipgloss.Style
	yellow lipgloss.Style
	red    lipgloss.Style
	dim    lipgloss.Style
}

func newPullStyles() pullStyles {
	return pullStyles{
		green:  lipgloss.NewStyle().Foreground(ui.SuccessColor),
		yellow: lipgloss.NewStyle().Foreground(ui.WarningColor),
		red:    lipgloss.NewStyle().Foreground(ui.ErrorColor),
		dim:    lipgloss.NewStyle().Foreground(ui.DimColor),
	}
}

func newPullHooks() pullsvc.Hooks {
	styles := newPullStyles()
	return pullsvc.Hooks{
		OnStart:       renderPullStart,
		OnSkip:        func(t pullsvc.Target, status string) { renderPullSkip(t, status, styles) },
		OnPulling:     func(t pullsvc.Target, originalBranch string) { renderPulling(t, originalBranch, styles) },
		OnResult:      func(result pullsvc.Result) { renderPullResult(result, styles) },
		OnChangedRefs: func(paths []string) { renderChangedRefs(paths, styles) },
		OnSummary:     func(summary pullsvc.Summary) { renderPullSummary(summary, styles) },
	}
}

func renderPullStart() {
	fmt.Println(ui.Info("Pulling all repos..."))
	fmt.Println()
}

func renderPullSkip(t pullsvc.Target, status string, styles pullStyles) {
	fmt.Printf("  %-30s %s\n", t.Name, styles.yellow.Render(status))
}

func renderPulling(t pullsvc.Target, originalBranch string, styles pullStyles) {
	if originalBranch != "" && originalBranch != "HEAD" && originalBranch != t.Branch {
		fmt.Printf("  %-30s %s  %s  pulling... ",
			t.Name, styles.dim.Render(t.Branch),
			styles.dim.Render(fmt.Sprintf("(was %s)", originalBranch)))
	} else {
		fmt.Printf("  %-30s %s  pulling... ",
			t.Name, styles.dim.Render(t.Branch))
	}
}

func renderPullResult(result pullsvc.Result, styles pullStyles) {
	switch result.Outcome {
	case pullsvc.OutcomePulled:
		fmt.Println(styles.green.Render(result.Status))
	case pullsvc.OutcomeSkipped:
		fmt.Println(styles.dim.Render(result.Status))
	case pullsvc.OutcomeFailed:
		fmt.Println(styles.red.Render(result.Status))
	}
}

func renderPullSummary(summary pullsvc.Summary, styles pullStyles) {
	fmt.Println()
	total := summary.Pulled + summary.Failed
	if total == 0 {
		fmt.Println(ui.Info("All repos are up-to-date, nothing to pull"))
	} else if summary.Failed == 0 {
		fmt.Println(styles.green.Render(fmt.Sprintf("Pulled %d/%d repos successfully", summary.Pulled, total)))
	} else {
		fmt.Println(styles.yellow.Render(fmt.Sprintf("Pulled %d/%d repos (%d failed)", summary.Pulled, total, summary.Failed)))
		for _, e := range summary.Errors {
			fmt.Println(styles.red.Render(e))
		}
	}

	if len(summary.ChangedRefs) > 0 {
		fmt.Println()
		fmt.Println(styles.dim.Render("  Run 'camp commit' to record these ref updates."))
	}
}

func renderChangedRefs(changed []string, styles pullStyles) {
	if len(changed) == 0 {
		return
	}

	fmt.Println()
	fmt.Println(styles.yellow.Render("  Submodule refs updated (not yet committed):"))
	for _, p := range changed {
		fmt.Printf("    %-30s (new commits)\n", git.SubmoduleDisplayName(p))
	}
}
