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
)

type followUpScope struct {
	projectName string
	label       string
}

type followUpTUIModel struct {
	ctx      context.Context
	root     string
	projects []project.Project
	cfg      *config.FreshConfig
	scopes   []followUpScope

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

	status    string
	statusErr bool
	width     int
	height    int
	quitting  bool
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

	program := tea.NewProgram(newFollowUpTUIModel(ctx, campRoot, projects, cfg), tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return camperrors.Wrap(err, "running fresh configuration TUI")
	}
	return nil
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
	m.rebuildScopes(globalFollowUpScope)
	return m
}

func (m *followUpTUIModel) rebuildScopes(selected string) {
	scopes := []followUpScope{{projectName: globalFollowUpScope, label: "Global defaults"}}
	names := make(map[string]struct{}, len(m.projects)+len(m.cfg.Projects))
	for _, p := range m.projects {
		names[p.Name] = struct{}{}
	}
	for name := range m.cfg.Projects {
		names[name] = struct{}{}
	}
	projectNames := make([]string, 0, len(names))
	for name := range names {
		projectNames = append(projectNames, name)
	}
	sort.Strings(projectNames)
	for _, name := range projectNames {
		label := name + " · inherits global"
		if pc, ok := m.cfg.Projects[name]; ok && pc.FollowUp != nil {
			label = fmt.Sprintf("%s · project override (%d)", name, len(pc.FollowUp))
		}
		scopes = append(scopes, followUpScope{projectName: name, label: label})
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

func (m *followUpTUIModel) refresh(selected string) error {
	cfg, err := config.LoadFreshConfig(m.ctx, m.root)
	if err != nil {
		return camperrors.Wrap(err, "loading fresh config")
	}
	m.cfg = cfg
	m.rebuildScopes(selected)
	return nil
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
	m.statusErr = false
}

func (m *followUpTUIModel) setError(err error) {
	m.status = err.Error()
	m.statusErr = true
}

func (m *followUpTUIModel) Init() tea.Cmd {
	return textinput.Blink
}

func configuredProjectNames(cfg *config.FreshConfig) []string {
	names := make([]string, 0, len(cfg.Projects))
	for name, pc := range cfg.Projects {
		if pc.FollowUp != nil {
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
