package fresh

import (
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// freshStepKind separates the three things a row in the fresh sequence can be.
// The distinction is what the configure TUI groups on: a user standing in the
// workflow needs to know whether a step is something they can change, and if
// so, how, before they press a key at it.
type freshStepKind int

const (
	// freshStepFixed always runs and has nothing to configure.
	freshStepFixed freshStepKind = iota
	// freshStepSetting is driven by a key in fresh.yaml.
	freshStepSetting
	// freshStepFollowUp is a user-defined command.
	freshStepFollowUp
	// freshStepDone is the terminal marker. It is fixed like freshStepFixed but
	// carries its own kind so the pane can group strictly by kind: it trails the
	// follow-ups, and folding it back in with the leading fixed steps would
	// scatter one section across two ends of the sequence.
	freshStepDone
)

// freshSettingKey names the fresh.yaml key behind a freshStepSetting.
type freshSettingKey int

const (
	freshSettingNone freshSettingKey = iota
	freshSettingPrune
	freshSettingPruneRemote
	freshSettingBranch
	freshSettingPushUpstream
)

// freshStepState is why a step will or will not run. "Off" and "unset" are
// deliberately distinct: a step nobody has configured is an invitation, while
// one explicitly turned off is a decision, and a step held off by a dependency
// is neither. Collapsing them into a single disabled state is what made the
// old pane show "Create working branch [off]" next to a step that was off only
// because no branch was named.
type freshStepState int

const (
	// freshStateOn will run.
	freshStateOn freshStepState = iota
	// freshStateOff was explicitly turned off.
	freshStateOff
	// freshStateUnset has never been configured.
	freshStateUnset
	// freshStateBlocked cannot run until another setting changes.
	freshStateBlocked
)

// freshWorkflowStep is the human-readable plan for one fresh cycle. It is
// deliberately shared by the TUI and show-workflow so the two surfaces cannot
// drift apart from executeFresh's actual order.
type freshWorkflowStep struct {
	Title   string
	Detail  string
	Enabled bool
	Kind    freshStepKind
	Setting freshSettingKey
	State   freshStepState
	// GlobalOnly marks a setting fresh.yaml accepts only at the campaign
	// level, so a project scope can say so rather than offer an edit that
	// would write a key fresh never reads.
	GlobalOnly bool
	Follow     *config.FollowUpConfig
}

// Configurable reports whether the step has anything the TUI can change in the
// currently selected scope.
func (s freshWorkflowStep) Configurable(projectScope bool) bool {
	switch s.Kind {
	case freshStepFollowUp:
		return true
	case freshStepSetting:
		if projectScope && s.GlobalOnly {
			return false
		}
		return true
	default:
		return false
	}
}

func buildFreshWorkflow(cfg *config.FreshConfig, projectName string) []freshWorkflowStep {
	if cfg == nil {
		cfg = &config.FreshConfig{}
	}

	branch := cfg.ResolveFreshBranch("", false, projectName)
	push := cfg.ResolveFreshPushUpstream(projectName)
	prune := cfg.ResolveFreshPrune()
	pruneRemote := cfg.ResolveFreshPruneRemote()

	steps := []freshWorkflowStep{
		{Title: "Safety checks", Detail: "no merge/rebase in progress; worktree is clean", Enabled: true},
		{Title: "Checkout default branch", Detail: "reclaim default branch from a clean worktree if needed, then check it out (detached origin/ fallback when dirty)", Enabled: true},
		{Title: "Pull", Detail: "fast-forward only", Enabled: true},
	}

	steps = append(steps,
		freshWorkflowStep{
			Title:      "Prune merged branches",
			Detail:     onOffDetail(prune, "remove merged branches and detached worktrees"),
			Enabled:    prune,
			Kind:       freshStepSetting,
			Setting:    freshSettingPrune,
			State:      boolState(prune),
			GlobalOnly: true,
		},
		freshWorkflowStep{
			Title:      "Prune remote tracking refs",
			Detail:     pruneRemoteDetail(prune, pruneRemote),
			Enabled:    prune && pruneRemote,
			Kind:       freshStepSetting,
			Setting:    freshSettingPruneRemote,
			State:      dependentState(prune, pruneRemote),
			GlobalOnly: true,
		},
		freshWorkflowStep{
			Title:   "Create working branch",
			Detail:  branchDetail(branch),
			Enabled: branch != "",
			Kind:    freshStepSetting,
			Setting: freshSettingBranch,
			State:   branchState(cfg, projectName, branch),
		},
		freshWorkflowStep{
			Title:   "Push branch upstream",
			Detail:  pushDetail(branch, push),
			Enabled: branch != "" && push,
			Kind:    freshStepSetting,
			Setting: freshSettingPushUpstream,
			State:   dependentState(branch != "", push),
		},
	)

	for _, follow := range cfg.ResolveFreshFollowUps(projectName) {
		step := follow
		steps = append(steps, freshWorkflowStep{
			Title:   "Follow-up: " + follow.Name,
			Detail:  formatFollowUpDetail(follow),
			Enabled: true,
			Kind:    freshStepFollowUp,
			Follow:  &step,
		})
	}

	steps = append(steps, freshWorkflowStep{
		Title:   "Ready to work",
		Detail:  "fresh completes successfully",
		Enabled: true,
		Kind:    freshStepDone,
	})
	return steps
}

func boolState(on bool) freshStepState {
	if on {
		return freshStateOn
	}
	return freshStateOff
}

// dependentState reports the state of a step whose own setting is on but whose
// dependency is not, so the pane can say "waiting on something else" instead
// of implying the user turned it off.
func dependentState(dependency, own bool) freshStepState {
	if !own {
		return freshStateOff
	}
	if !dependency {
		return freshStateBlocked
	}
	return freshStateOn
}

// branchState distinguishes three empty-looking answers:
//
//   - a resolved non-empty branch → on
//   - a project-scope explicit branch: "" (pointer to empty string) → off
//   - never configured (no key, or inherit of an empty global) → unset
//
// The resolved string alone cannot tell off from unset: both resolve to "".
// Project config stores Branch as *string so the pointer's presence is the
// "I decided no branch" signal SetFreshBranch carefully preserves.
func branchState(cfg *config.FreshConfig, projectName, branch string) freshStepState {
	if branch != "" {
		return freshStateOn
	}
	if projectName != "" {
		if pc, ok := cfg.Projects[projectName]; ok && pc.Branch != nil && *pc.Branch == "" {
			return freshStateOff
		}
	}
	return freshStateUnset
}

func onOffDetail(on bool, detail string) string {
	if on {
		return detail
	}
	return "off · " + detail
}

func pruneRemoteDetail(prune, pruneRemote bool) string {
	switch {
	case !pruneRemote:
		return "off · refresh and remove stale refs"
	case !prune:
		return "needs branch pruning · refresh and remove stale refs"
	default:
		return "refresh and remove stale refs"
	}
}

func branchDetail(branch string) string {
	if branch == "" {
		return "no branch configured · fresh stays on the default branch"
	}
	return "create " + branch
}

func pushDetail(branch string, push bool) string {
	switch {
	case !push:
		return "off · new branches stay local"
	case branch == "":
		return "needs a working branch · push with --set-upstream"
	default:
		return "push " + branch + " with --set-upstream"
	}
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
			title += " (" + stepStateWord(step.State) + ")"
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

func stepStateWord(state freshStepState) string {
	switch state {
	case freshStateUnset:
		return "not configured"
	case freshStateBlocked:
		return "blocked"
	default:
		return "off"
	}
}
