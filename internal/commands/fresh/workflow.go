package fresh

import (
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// freshWorkflowStep is the human-readable plan for one fresh cycle. It is
// deliberately shared by the TUI and show-workflow so the two surfaces cannot
// drift apart from executeFresh's actual order.
type freshWorkflowStep struct {
	Title   string
	Detail  string
	Enabled bool
	Follow  *config.FollowUpConfig
}

func buildFreshWorkflow(cfg *config.FreshConfig, projectName string) []freshWorkflowStep {
	if cfg == nil {
		cfg = &config.FreshConfig{}
	}

	branch := cfg.ResolveFreshBranch("", false, projectName)
	push := cfg.ResolveFreshPushUpstream(projectName)
	prune := cfg.ResolveFreshPrune()
	pruneRemote := cfg.ResolveFreshPruneRemote()
	branchDetail := "not configured"
	if branch != "" {
		branchDetail = "create " + branch
	}
	pushDetail := "after creating the working branch"
	if branch != "" && !push {
		pushDetail = "disabled in fresh settings"
	}

	steps := []freshWorkflowStep{
		{Title: "Safety checks", Detail: "no merge/rebase in progress; worktree is clean", Enabled: true},
		{Title: "Checkout default branch", Detail: "use the repository's default branch (or a safe detached ref)", Enabled: true},
		{Title: "Pull", Detail: "fast-forward only", Enabled: true},
		{Title: "Prune merged branches", Detail: "remove merged branches and detached worktrees", Enabled: prune},
		{Title: "Prune remote tracking refs", Detail: "refresh and remove stale refs", Enabled: prune && pruneRemote},
		{Title: "Create working branch", Detail: branchDetail, Enabled: branch != ""},
		{Title: "Push branch upstream", Detail: pushDetail, Enabled: branch != "" && push},
	}

	for _, follow := range cfg.ResolveFreshFollowUps(projectName) {
		step := follow
		steps = append(steps, freshWorkflowStep{
			Title:   "Follow-up: " + follow.Name,
			Detail:  formatFollowUpDetail(follow),
			Enabled: true,
			Follow:  &step,
		})
	}

	steps = append(steps, freshWorkflowStep{
		Title:   "Ready to work",
		Detail:  "fresh completes successfully",
		Enabled: true,
	})
	return steps
}

func formatFollowUpDetail(step config.FollowUpConfig) string {
	detail := "$ " + step.Run
	if step.Dir != "" {
		detail += " · in " + step.Dir
	}
	if step.ContinueOnError {
		detail += " · continue on failure"
	} else {
		detail += " · stop on failure"
	}
	return detail
}

func workflowScopeLabel(projectName string) string {
	if projectName == "" {
		return "global defaults"
	}
	return "project " + projectName
}

func printFreshWorkflow(w io.Writer, cfg *config.FreshConfig, projectName string) error {
	if _, err := fmt.Fprintf(w, "%s\n", ui.Header("camp fresh workflow · "+workflowScopeLabel(projectName))); err != nil {
		return err
	}
	if projectName == "" {
		if _, err := fmt.Fprintln(w, ui.Dim("Pass a project name to see its resolved overrides.")); err != nil {
			return err
		}
	}

	for i, step := range buildFreshWorkflow(cfg, projectName) {
		icon := ui.SuccessIcon()
		if !step.Enabled {
			icon = ui.WarningIcon()
		}
		title := step.Title
		if !step.Enabled {
			title += " (disabled)"
		}
		if _, err := fmt.Fprintf(w, "  %s %d. %s\n", icon, i+1, ui.Value(title)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "     %s\n", ui.Dim(step.Detail)); err != nil {
			return err
		}
	}
	return nil
}
