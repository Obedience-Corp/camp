package fresh

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

const globalFollowUpScope = "__global__"

type followUpPane int

const (
	followUpScopesPane followUpPane = iota
	followUpWorkflowPane
)

type followUpOverlay int

const (
	followUpNoOverlay followUpOverlay = iota
	followUpFormOverlay
	followUpDeleteOverlay
	followUpHelpOverlay
	followUpSettingOverlay
)

// freshSettingAction is what saving the settings editor does to the selected
// scope's key. Inherit clears it, which is a distinct outcome from writing
// false: FreshProjectConfig stores pointers precisely so a project can leave a
// key unset and follow the global value as it changes.
type freshSettingAction int

const (
	freshSettingInherit freshSettingAction = iota
	freshSettingOn
	freshSettingOff
	freshSettingNoBranch
	freshSettingCustomBranch
)

type freshSettingOption struct {
	label  string
	action freshSettingAction
}

// statusLevel separates a completed change from a note about why a key did
// nothing. A refusal rendered in the same red as a genuine failure trains
// users to ignore the status line, and most refusals here are only the TUI
// saying a row has nothing to configure.
type statusLevel int

const (
	statusOK statusLevel = iota
	statusNotice
	statusError
)

type followUpScope struct {
	projectName string
	// name is the scope's display name on its own, without decoration, so the
	// renderer can drop badges rather than the identity when space is short.
	name string
	// overrideCount is how many fresh.yaml keys this project sets for itself
	// (branch, push_upstream, follow-up list, …), via ProjectOverrideKeys.
	// override is true when that count is non-zero. An explicit empty
	// follow-up list still counts as one key, because FollowUp != nil.
	override      bool
	overrideCount int
	// current marks the project detected from the working directory.
	current bool
}

type followUpTUIModel struct {
	ctx      context.Context
	root     string
	projects []project.Project
	cfg      *config.FreshConfig
	scopes   []followUpScope
	// cwdProject is the project camp fresh would act on from this directory,
	// or empty at the campaign root.
	cwdProject string

	pane        followUpPane
	scopeCursor int
	stepCursor  int

	overlay       followUpOverlay
	inputs        [3]textinput.Model
	formField     int
	formEditName  string
	formContinue  bool
	formError     string
	pendingDelete string

	// settingStep is the workflow row the settings editor is open on, with the
	// options and cursor that editor is showing.
	settingStep    freshWorkflowStep
	settingOptions []freshSettingOption
	settingChoice  int
	settingInput   textinput.Model
	settingError   string

	status      string
	statusLevel statusLevel
	width       int
	height      int
	quitting    bool
}

// runConfigureTUI is the human-facing entry point for `camp fresh configure`.
// The child commands remain available for scripts and agents that need a
// non-interactive interface.
func runConfigureTUI(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if !ui.IsTerminal() {
		return camperrors.Wrap(camperrors.ErrInvalidInput,
			"fresh configure requires an interactive terminal; use configure show|add|remove for automation")
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}
	projects, err := project.List(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "listing campaign projects")
	}
	cfg, err := config.LoadFreshConfig(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "loading fresh config")
	}

	scope, err := resolveConfigureScope(ctx, campRoot, configureProjectFlag)
	if err != nil {
		return err
	}

	model := newFollowUpTUIModel(ctx, campRoot, projects, cfg)
	model.selectProjectScope(scope)

	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return camperrors.Wrap(err, "running fresh configuration TUI")
	}
	return nil
}

// resolveConfigureScope names the project the TUI should open on. An explicit
// --project wins and must resolve; otherwise the working directory decides,
// through the same resolver `camp fresh` uses to pick its target. Sharing that
// resolver is what keeps the two commands honest: whatever name fresh will act
// on is the name whose overrides you are editing. Outside a project the
// campaign root is the honest answer, so detection failure is not an error.
func resolveConfigureScope(ctx context.Context, campRoot, flagProject string) (string, error) {
	if flagProject != "" {
		resolved, err := project.Resolve(ctx, campRoot, flagProject)
		if err != nil {
			return "", err
		}
		return resolved.Name, nil
	}
	resolved, err := project.Resolve(ctx, campRoot, "")
	if err != nil {
		return "", nil
	}
	return resolved.Name, nil
}

func newFollowUpTUIModel(ctx context.Context, root string, projects []project.Project, cfg *config.FreshConfig) *followUpTUIModel {
	m := &followUpTUIModel{
		ctx:      ctx,
		root:     root,
		projects: projects,
		cfg:      cfg,
	}
	for i := range m.inputs {
		m.inputs[i] = textinput.New()
		m.inputs[i].Prompt = "  "
		m.inputs[i].CharLimit = 256
	}
	m.inputs[0].Placeholder = "install"
	m.inputs[1].Placeholder = "npm install"
	m.inputs[2].Placeholder = "optional, relative to project root"
	m.settingInput = textinput.New()
	m.settingInput.Prompt = "  "
	m.settingInput.CharLimit = 256
	m.settingInput.Placeholder = "feat/my-work"
	m.rebuildScopes(globalFollowUpScope)
	return m
}

func (m *followUpTUIModel) rebuildScopes(selected string) {
	scopes := []followUpScope{{projectName: globalFollowUpScope, name: "Global defaults"}}
	names := make(map[string]struct{}, len(m.projects)+len(m.cfg.Projects)+1)
	for _, p := range m.projects {
		names[p.Name] = struct{}{}
	}
	for name := range m.cfg.Projects {
		names[name] = struct{}{}
	}
	// The detected project may be a worktree or a linked repo that project.List
	// does not enumerate. It still needs a row: it is the scope the user is
	// standing in, and fresh will read overrides under exactly this name.
	if m.cwdProject != "" {
		names[m.cwdProject] = struct{}{}
	}
	projectNames := make([]string, 0, len(names))
	for name := range names {
		projectNames = append(projectNames, name)
	}
	sort.Strings(projectNames)
	for _, name := range projectNames {
		scope := followUpScope{
			projectName: name,
			name:        name,
			current:     name == m.cwdProject,
		}
		// Count every key the project sets, not just its follow-ups. A project
		// that overrides only its branch is still a project that deviates from
		// the global defaults, and the badge is the one place the scopes pane
		// says so.
		if pc, ok := m.cfg.Projects[name]; ok {
			if keys := config.ProjectOverrideKeys(pc); keys > 0 {
				scope.override = true
				scope.overrideCount = keys
			}
		}
		scopes = append(scopes, scope)
	}
	m.scopes = scopes
	m.scopeCursor = 0
	for i, scope := range scopes {
		if scope.projectName == selected {
			m.scopeCursor = i
			break
		}
	}
	m.stepCursor = min(m.stepCursor, max(len(m.workflowSteps())-1, 0))
}

// selectProjectScope opens the TUI on projectName. An empty name, or one with
// no matching row, leaves the global scope selected.
func (m *followUpTUIModel) selectProjectScope(projectName string) {
	if projectName == "" {
		return
	}
	m.cwdProject = projectName
	m.rebuildScopes(projectName)
	if m.selectedScope() == projectName {
		m.setStatus(fmt.Sprintf("editing %s · detected from the current directory", workflowScopeLabel(projectName)))
	}
}

func (m *followUpTUIModel) refresh(selected string) error {
	cfg, err := config.LoadFreshConfig(m.ctx, m.root)
	if err != nil {
		return camperrors.Wrap(err, "loading fresh config")
	}
	m.cfg = cfg
	m.rebuildScopes(selected)
	return nil
}

// scopeInheritsGlobal reports whether the selected project scope is showing
// the global list because it has none of its own. Mutating such a scope forks
// the global steps into a project override, so the callers that change
// configuration have to say so.
func (m *followUpTUIModel) scopeInheritsGlobal() bool {
	scope := m.selectedScope()
	if scope == globalFollowUpScope {
		return false
	}
	pc, ok := m.cfg.Projects[scope]
	return !ok || pc.FollowUp == nil
}

func (m *followUpTUIModel) selectedScope() string {
	if len(m.scopes) == 0 {
		return globalFollowUpScope
	}
	return m.scopes[m.scopeCursor].projectName
}

func (m *followUpTUIModel) workflowSteps() []freshWorkflowStep {
	return buildFreshWorkflow(m.cfg, scopeProjectName(m.selectedScope()))
}

func (m *followUpTUIModel) effectiveFollowUps() []config.FollowUpConfig {
	return m.cfg.ResolveFreshFollowUps(scopeProjectName(m.selectedScope()))
}

func (m *followUpTUIModel) selectedFollowUp() (int, config.FollowUpConfig, bool) {
	steps := m.workflowSteps()
	if m.stepCursor < 0 || m.stepCursor >= len(steps) || steps[m.stepCursor].Follow == nil {
		return -1, config.FollowUpConfig{}, false
	}
	for i, follow := range m.effectiveFollowUps() {
		if follow.Name == steps[m.stepCursor].Follow.Name {
			return i, follow, true
		}
	}
	return -1, config.FollowUpConfig{}, false
}

func (m *followUpTUIModel) setStatus(message string) {
	m.status = message
	m.statusLevel = statusOK
}

// setNotice reports that a key did nothing here, which is guidance rather than
// failure and so is not rendered as an error.
func (m *followUpTUIModel) setNotice(message string) {
	m.status = message
	m.statusLevel = statusNotice
}

func (m *followUpTUIModel) setError(err error) {
	m.status = err.Error()
	m.statusLevel = statusError
}

// selectedStep returns the workflow row under the cursor.
func (m *followUpTUIModel) selectedStep() (freshWorkflowStep, bool) {
	steps := m.workflowSteps()
	if m.stepCursor < 0 || m.stepCursor >= len(steps) {
		return freshWorkflowStep{}, false
	}
	return steps[m.stepCursor], true
}

func (m *followUpTUIModel) inProjectScope() bool {
	return m.selectedScope() != globalFollowUpScope
}

// settingOptionsFor builds the choices the settings editor offers for a step.
// A project scope gains an inherit option, since clearing the key there is a
// real third outcome. The global scope has no one to inherit from, but bool
// keys still need a third choice: "default" clears the key so the built-in
// applies, which is distinct from writing an explicit true/false.
func (m *followUpTUIModel) settingOptionsFor(step freshWorkflowStep) []freshSettingOption {
	project := m.inProjectScope()

	if step.Setting == freshSettingBranch {
		options := make([]freshSettingOption, 0, 3)
		if project {
			options = append(options, freshSettingOption{
				label:  "inherit from global · " + branchSummary(m.cfg.Branch),
				action: freshSettingInherit,
			})
		}
		return append(options,
			freshSettingOption{label: "no branch · stay on the default branch", action: freshSettingNoBranch},
			freshSettingOption{label: "create a branch...", action: freshSettingCustomBranch},
		)
	}

	options := make([]freshSettingOption, 0, 3)
	switch {
	case project && !step.GlobalOnly:
		options = append(options, freshSettingOption{
			label:  "inherit from global · " + onOffWord(m.globalBoolValue(step.Setting)),
			action: freshSettingInherit,
		})
	case !project:
		// Built-in defaults for prune / prune_remote / push_upstream are true.
		// Name the default, not the currently resolved value, so an explicit
		// "off" does not make the default option read "default · off".
		options = append(options, freshSettingOption{
			label:  "default · " + onOffWord(builtInBoolDefault(step.Setting)),
			action: freshSettingInherit,
		})
	}
	return append(options,
		freshSettingOption{label: "on", action: freshSettingOn},
		freshSettingOption{label: "off", action: freshSettingOff},
	)
}

// currentSettingAction is the option that matches what the selected scope
// stores today, so the editor opens on the current answer rather than on a
// default that would silently rewrite the key if the user just pressed enter.
//
// For global bools this must inspect the stored pointer, not the resolved
// value: Resolve* collapses a missing key to the built-in default (true), and
// mapping that to "on" would open the editor on an option that writes an
// explicit true into a previously absent key.
func (m *followUpTUIModel) currentSettingAction(step freshWorkflowStep) freshSettingAction {
	scope := scopeProjectName(m.selectedScope())
	pc, hasProject := m.cfg.Projects[scope]

	switch step.Setting {
	case freshSettingBranch:
		if scope != "" {
			if !hasProject || pc.Branch == nil {
				return freshSettingInherit
			}
			if *pc.Branch == "" {
				return freshSettingNoBranch
			}
			return freshSettingCustomBranch
		}
		if m.cfg.Branch == "" {
			return freshSettingNoBranch
		}
		return freshSettingCustomBranch
	case freshSettingPushUpstream:
		if scope != "" {
			if !hasProject || pc.PushUpstream == nil {
				return freshSettingInherit
			}
			return boolAction(*pc.PushUpstream)
		}
		if m.cfg.PushUpstream == nil {
			return freshSettingInherit
		}
		return boolAction(*m.cfg.PushUpstream)
	case freshSettingPrune:
		if m.cfg.Prune == nil {
			return freshSettingInherit
		}
		return boolAction(*m.cfg.Prune)
	case freshSettingPruneRemote:
		if m.cfg.PruneRemote == nil {
			return freshSettingInherit
		}
		return boolAction(*m.cfg.PruneRemote)
	}
	return freshSettingInherit
}

// builtInBoolDefault is the value Resolve* uses when a global bool key is
// absent. Kept local so option labels do not re-derive it from a currently
// written override.
func builtInBoolDefault(setting freshSettingKey) bool {
	switch setting {
	case freshSettingPushUpstream, freshSettingPrune, freshSettingPruneRemote:
		return true
	default:
		return false
	}
}

// settingScopeBranch is the branch this scope stores on its own, used to seed
// the text input when the editor opens.
//
// It deliberately does not fall back to the global branch. Seeding the field
// with an inherited value puts text in it that the user never typed and cannot
// tell apart from their own: switching to "create a branch" and typing then
// appends, which silently produced branch names like "developfeat/storefront".
// A scope with no branch of its own opens on an empty field.
func (m *followUpTUIModel) settingScopeBranch() string {
	scope := scopeProjectName(m.selectedScope())
	if scope == "" {
		return m.cfg.Branch
	}
	if pc, ok := m.cfg.Projects[scope]; ok && pc.Branch != nil {
		return *pc.Branch
	}
	return ""
}

func (m *followUpTUIModel) globalBoolValue(setting freshSettingKey) bool {
	switch setting {
	case freshSettingPushUpstream:
		return m.cfg.ResolveFreshPushUpstream("")
	case freshSettingPrune:
		return m.cfg.ResolveFreshPrune()
	case freshSettingPruneRemote:
		return m.cfg.ResolveFreshPruneRemote()
	}
	return false
}

func boolAction(on bool) freshSettingAction {
	if on {
		return freshSettingOn
	}
	return freshSettingOff
}

func onOffWord(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func branchSummary(branch string) string {
	if branch == "" {
		return "no branch"
	}
	return branch
}

// settingTitle names the fresh.yaml key a settings row edits, so the editor
// header matches what the user would search for in the file.
func settingTitle(setting freshSettingKey) string {
	switch setting {
	case freshSettingPrune:
		return "prune"
	case freshSettingPruneRemote:
		return "prune_remote"
	case freshSettingBranch:
		return "branch"
	case freshSettingPushUpstream:
		return "push_upstream"
	}
	return ""
}

func (m *followUpTUIModel) Init() tea.Cmd {
	return textinput.Blink
}

// configuredProjectNames lists projects that set any fresh.yaml key of their
// own. It uses ProjectOverrideKeys so a branch-only override is not treated as
// unconfigured — the same rule the scopes badge applies.
func configuredProjectNames(cfg *config.FreshConfig) []string {
	names := make([]string, 0, len(cfg.Projects))
	for name, pc := range cfg.Projects {
		if config.ProjectOverrideKeys(pc) > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func followUpsForScope(cfg *config.FreshConfig, scope string) []config.FollowUpConfig {
	if scope == globalFollowUpScope {
		return cfg.FollowUp
	}
	return cfg.Projects[scope].FollowUp
}

func scopeProjectName(scope string) string {
	if scope == globalFollowUpScope {
		return ""
	}
	return scope
}

func scopeDescription(scope string) string {
	if scope == globalFollowUpScope {
		return "the global default"
	}
	return "project " + scope
}

func requiredField(label string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return camperrors.NewValidation(label, "must not be empty", nil)
		}
		return nil
	}
}
