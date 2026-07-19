package fresh

import (
	"context"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/project"
)

func settingsModel(t *testing.T, cfg *config.FreshConfig) *followUpTUIModel {
	t.Helper()
	return newFollowUpTUIModel(context.Background(), t.TempDir(),
		[]project.Project{{Name: "api"}, {Name: "web"}}, cfg)
}

// stepIndexFor locates a step by the fresh.yaml key behind it, so the tests do
// not encode the sequence position of a step that may gain neighbors later.
func stepIndexFor(t *testing.T, m *followUpTUIModel, setting freshSettingKey) int {
	t.Helper()
	return stepIndexBySetting(t, m.workflowSteps(), setting)
}

// stepIndexBySetting is the raw-slice form of stepIndexFor for tests that call
// buildFreshWorkflow directly without a TUI model.
func stepIndexBySetting(t *testing.T, steps []freshWorkflowStep, setting freshSettingKey) int {
	t.Helper()
	for i, step := range steps {
		if step.Kind == freshStepSetting && step.Setting == setting {
			return i
		}
	}
	t.Fatalf("no settings step for key %v", setting)
	return -1
}

func TestBuildFreshWorkflowClassifiesSteps(t *testing.T) {
	cfg := &config.FreshConfig{
		FollowUp: []config.FollowUpConfig{{Name: "install", Run: "npm install"}},
	}
	steps := buildFreshWorkflow(cfg, "")

	kinds := map[freshStepKind]int{}
	for _, step := range steps {
		kinds[step.Kind]++
	}
	if kinds[freshStepFixed] != 3 {
		t.Errorf("fixed steps = %d, want 3", kinds[freshStepFixed])
	}
	if kinds[freshStepSetting] != 4 {
		t.Errorf("setting steps = %d, want 4", kinds[freshStepSetting])
	}
	if kinds[freshStepFollowUp] != 1 {
		t.Errorf("follow-up steps = %d, want 1", kinds[freshStepFollowUp])
	}
	if kinds[freshStepDone] != 1 {
		t.Errorf("done steps = %d, want 1", kinds[freshStepDone])
	}
}

// An unconfigured branch and a branch turned off are different answers, and
// the pane renders them differently, so the state has to distinguish them.
func TestBranchStateSeparatesUnsetFromOff(t *testing.T) {
	unset := buildFreshWorkflow(&config.FreshConfig{}, "")
	idx := stepIndexBySetting(t, unset, freshSettingBranch)
	if got := unset[idx].State; got != freshStateUnset {
		t.Fatalf("unconfigured branch state = %v, want unset", got)
	}
	if got := stepGlyph(unset[idx]); got != "○" {
		t.Errorf("unconfigured branch glyph = %q, want ○", got)
	}

	configured := buildFreshWorkflow(&config.FreshConfig{Branch: "develop"}, "")
	if got := configured[idx].State; got != freshStateOn {
		t.Fatalf("configured branch state = %v, want on", got)
	}
}

// Push upstream defaults to true, so with no branch it is off as a consequence
// rather than by choice. Reporting that as "off" told users they had disabled
// something they never touched.
func TestPushUpstreamBlockedWithoutBranch(t *testing.T) {
	steps := buildFreshWorkflow(&config.FreshConfig{}, "")
	push := steps[stepIndexBySetting(t, steps, freshSettingPushUpstream)]
	if push.State != freshStateBlocked {
		t.Fatalf("push state without a branch = %v, want blocked", push.State)
	}
	if !strings.Contains(push.Detail, "needs a working branch") {
		t.Errorf("push detail = %q, want it to name the missing branch", push.Detail)
	}

	off := false
	steps = buildFreshWorkflow(&config.FreshConfig{Branch: "develop", PushUpstream: &off}, "")
	if got := steps[stepIndexBySetting(t, steps, freshSettingPushUpstream)].State; got != freshStateOff {
		t.Fatalf("explicitly disabled push state = %v, want off", got)
	}
}

func TestConfigurableExcludesGlobalOnlyKeysInProjectScope(t *testing.T) {
	steps := buildFreshWorkflow(&config.FreshConfig{}, "api")
	prune := steps[stepIndexBySetting(t, steps, freshSettingPrune)]
	push := steps[stepIndexBySetting(t, steps, freshSettingPushUpstream)]

	if prune.Configurable(true) {
		t.Error("prune reported configurable in a project scope; fresh only reads it globally")
	}
	if !prune.Configurable(false) {
		t.Error("prune reported unconfigurable in the global scope")
	}
	if !push.Configurable(true) {
		t.Error("push_upstream reported unconfigurable in a project scope")
	}
}

func TestSettingOptionsAddInheritOnlyInProjectScope(t *testing.T) {
	m := settingsModel(t, &config.FreshConfig{Branch: "develop"})
	branchStep := m.workflowSteps()[stepIndexFor(t, m, freshSettingBranch)]

	global := m.settingOptionsFor(branchStep)
	if len(global) != 2 {
		t.Fatalf("global branch options = %d, want 2", len(global))
	}
	for _, option := range global {
		if option.action == freshSettingInherit {
			t.Fatal("global scope offered an inherit option with nothing to inherit from")
		}
	}

	m.rebuildScopes("api")
	project := m.settingOptionsFor(branchStep)
	if len(project) != 3 || project[0].action != freshSettingInherit {
		t.Fatalf("project branch options = %+v, want inherit first", project)
	}
	if !strings.Contains(project[0].label, "develop") {
		t.Errorf("inherit label %q does not name the value it inherits", project[0].label)
	}
}

// An absent global bool is the built-in default, not an explicit "on". The
// editor must open on "default" so a pure enter leaves the file alone.
func TestGlobalBoolOptionsOfferDefaultAndOpenOnAbsentKey(t *testing.T) {
	m := settingsModel(t, &config.FreshConfig{})
	pruneStep := m.workflowSteps()[stepIndexFor(t, m, freshSettingPrune)]

	options := m.settingOptionsFor(pruneStep)
	if len(options) != 3 || options[0].action != freshSettingInherit {
		t.Fatalf("global prune options = %+v, want default first", options)
	}
	if !strings.Contains(options[0].label, "default") {
		t.Errorf("first option label %q does not say default", options[0].label)
	}
	if got := m.currentSettingAction(pruneStep); got != freshSettingInherit {
		t.Fatalf("absent prune opens on action %v, want default/inherit", got)
	}

	on := true
	m.cfg.Prune = &on
	if got := m.currentSettingAction(pruneStep); got != freshSettingOn {
		t.Fatalf("explicit prune true opens on action %v, want on", got)
	}
	off := false
	m.cfg.Prune = &off
	if got := m.currentSettingAction(pruneStep); got != freshSettingOff {
		t.Fatalf("explicit prune false opens on action %v, want off", got)
	}
}

func TestSettingUnchangedShortCircuitsSave(t *testing.T) {
	m := settingsModel(t, &config.FreshConfig{})
	m.stepCursor = stepIndexFor(t, m, freshSettingPrune)
	step, _ := m.selectedStep()
	m.openSettingEditor(step)

	// Absent key + default selected → no write, notice status.
	cmd := m.saveSettingEditor()
	if cmd != nil {
		t.Fatal("unchanged save returned a command")
	}
	if m.overlay != followUpNoOverlay {
		t.Fatal("unchanged save left the editor open")
	}
	if m.statusLevel != statusNotice {
		t.Errorf("status level = %v, want notice", m.statusLevel)
	}
	if !strings.Contains(m.status, "nothing written") {
		t.Errorf("status %q does not say nothing was written", m.status)
	}
}

// Regression: the editor used to seed the branch input from the resolved
// branch, so a project inheriting "develop" opened with "develop" already in
// the field. Choosing "create a branch" and typing appended to it, writing
// names like "developfeat/storefront".
func TestSettingEditorDoesNotSeedInheritedBranch(t *testing.T) {
	m := settingsModel(t, &config.FreshConfig{Branch: "develop"})
	m.rebuildScopes("api")

	m.stepCursor = stepIndexFor(t, m, freshSettingBranch)
	step, _ := m.selectedStep()
	m.openSettingEditor(step)

	if got := m.settingInput.Value(); got != "" {
		t.Fatalf("branch input seeded with %q for an inheriting project, want empty", got)
	}
	if got := m.selectedSettingAction(); got != freshSettingInherit {
		t.Errorf("editor opened on action %v, want inherit", got)
	}
}

func TestSettingEditorSeedsOwnBranch(t *testing.T) {
	own := "feat/api"
	m := settingsModel(t, &config.FreshConfig{
		Branch:   "develop",
		Projects: map[string]config.FreshProjectConfig{"api": {Branch: &own}},
	})
	m.rebuildScopes("api")

	m.stepCursor = stepIndexFor(t, m, freshSettingBranch)
	step, _ := m.selectedStep()
	m.openSettingEditor(step)

	if got := m.settingInput.Value(); got != own {
		t.Fatalf("branch input = %q, want the project's own branch %q", got, own)
	}
	if got := m.selectedSettingAction(); got != freshSettingCustomBranch {
		t.Errorf("editor opened on action %v, want custom branch", got)
	}
}

// A custom branch with no name would write an empty string, which reads back
// as "no branch" and silently contradicts the option the user picked.
func TestResolveBranchActionRejectsEmptyCustomBranch(t *testing.T) {
	m := settingsModel(t, &config.FreshConfig{})
	m.settingInput.SetValue("   ")

	if _, _, err := m.resolveBranchAction(freshSettingCustomBranch); err == nil {
		t.Fatal("empty custom branch accepted")
	}

	branch, _, err := m.resolveBranchAction(freshSettingNoBranch)
	if err != nil {
		t.Fatalf("no-branch rejected: %v", err)
	}
	if branch == nil || *branch != "" {
		t.Errorf("no-branch produced %v, want a pointer to the empty string", branch)
	}

	if branch, _, err := m.resolveBranchAction(freshSettingInherit); err != nil || branch != nil {
		t.Errorf("inherit produced (%v, %v), want (nil, nil)", branch, err)
	}
}

func TestBoolForAction(t *testing.T) {
	if got := boolForAction(freshSettingInherit); got != nil {
		t.Errorf("inherit = %v, want nil so the key is cleared", got)
	}
	if got := boolForAction(freshSettingOn); got == nil || !*got {
		t.Errorf("on = %v, want true", got)
	}
	if got := boolForAction(freshSettingOff); got == nil || *got {
		t.Errorf("off = %v, want false", got)
	}
}

// Writing prune under a project would produce a fresh.yaml that looks
// configured and changes nothing, because fresh resolves prune globally.
func TestActivateGlobalOnlySettingRedirectsInsteadOfWriting(t *testing.T) {
	m := settingsModel(t, &config.FreshConfig{})
	m.rebuildScopes("api")
	m.pane = followUpWorkflowPane
	m.stepCursor = stepIndexFor(t, m, freshSettingPrune)

	m.activateSelectedStep()

	if m.overlay != followUpNoOverlay {
		t.Fatal("opened a settings editor for a campaign-wide key inside a project scope")
	}
	if m.statusLevel != statusNotice {
		t.Errorf("status level = %v, want a notice rather than an error", m.statusLevel)
	}
	if !strings.Contains(m.status, "prune") || !strings.Contains(m.status, "Global defaults") {
		t.Errorf("status %q does not point at where the key lives", m.status)
	}

	m.rebuildScopes(globalFollowUpScope)
	m.stepCursor = stepIndexFor(t, m, freshSettingPrune)
	m.activateSelectedStep()
	if m.overlay != followUpSettingOverlay {
		t.Fatal("global scope did not open the editor for prune")
	}
}

func TestActivateFixedStepIsANoticeNotAnError(t *testing.T) {
	m := settingsModel(t, &config.FreshConfig{})
	m.pane = followUpWorkflowPane
	m.stepCursor = 0

	m.activateSelectedStep()

	if m.overlay != followUpNoOverlay {
		t.Fatal("a fixed step opened an editor")
	}
	if m.statusLevel == statusError {
		t.Errorf("pressing enter on a fixed step reported an error: %q", m.status)
	}
	if !strings.Contains(m.status, "Safety checks") {
		t.Errorf("status %q does not name the step", m.status)
	}
}

// The pane groups by kind, and the follow-up section has to survive being
// empty: it is the only place that says adding a step is possible.
func TestWorkflowRowsAlwaysShowFollowUpSection(t *testing.T) {
	m := settingsModel(t, &config.FreshConfig{})
	steps := m.workflowSteps()
	rows := m.workflowRows(steps)

	headers := make([]string, 0, 3)
	for _, row := range rows {
		if row.stepIdx < 0 {
			headers = append(headers, row.header)
		}
	}
	want := []string{"Sync", "Settings", "Follow-ups"}
	if len(headers) != len(want) {
		t.Fatalf("headers = %v, want %v", headers, want)
	}
	for i := range want {
		if headers[i] != want[i] {
			t.Fatalf("headers = %v, want %v", headers, want)
		}
	}

	// Every step still gets exactly one row, in order, so the numbering the
	// pane prints stays the execution order.
	seen := 0
	for _, row := range rows {
		if row.stepIdx < 0 {
			continue
		}
		if row.stepIdx != seen {
			t.Fatalf("row %d carries step %d, want %d", seen, row.stepIdx, seen)
		}
		seen++
	}
	if seen != len(steps) {
		t.Fatalf("rendered %d steps, want %d", seen, len(steps))
	}
}

func TestWorkflowRowsPlaceEmptyFollowUpSectionBeforeDone(t *testing.T) {
	m := settingsModel(t, &config.FreshConfig{})
	rows := m.workflowRows(m.workflowSteps())

	last := rows[len(rows)-1]
	if last.stepIdx < 0 {
		t.Fatal("the terminal row is a header, not the Ready to work step")
	}
	header := rows[len(rows)-2]
	if header.stepIdx >= 0 || header.header != "Follow-ups" {
		t.Fatalf("row before the terminal step = %+v, want the Follow-ups header", header)
	}
	if !strings.Contains(header.hint, "a: add") {
		t.Errorf("empty follow-up hint %q does not say how to add one", header.hint)
	}
}
