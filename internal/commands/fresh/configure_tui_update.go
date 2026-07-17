package fresh

import (
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *followUpTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if m.overlay != followUpNoOverlay {
			return m.updateOverlay(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m *followUpTUIModel) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key != "r" && m.status != "" {
		m.status = ""
	}

	switch key {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		if m.pane == followUpWorkflowPane {
			m.pane = followUpScopesPane
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "left", "h":
		m.pane = followUpScopesPane
	case "right", "l", "tab":
		m.pane = followUpWorkflowPane
	case "shift+tab":
		m.pane = followUpScopesPane
	case "enter":
		if m.pane == followUpScopesPane {
			m.pane = followUpWorkflowPane
		}
	case "a":
		m.openFollowUpForm(false, config.FollowUpConfig{})
		return m, textinput.Blink
	case "e":
		_, entry, ok := m.selectedFollowUp()
		if ok {
			m.openFollowUpForm(true, entry)
			return m, textinput.Blink
		}
		m.setError(camperrors.New("select a follow-up step to edit"))
	case "d":
		_, entry, ok := m.selectedFollowUp()
		if ok {
			m.pendingDelete = entry.Name
			m.overlay = followUpDeleteOverlay
			return m, nil
		}
		m.setError(camperrors.New("select a follow-up step to delete"))
	case "r":
		selected := m.selectedScope()
		if err := m.refresh(selected); err != nil {
			m.setError(err)
		} else {
			m.setStatus("reloaded fresh.yaml")
		}
	case "?":
		m.overlay = followUpHelpOverlay
	}
	return m, nil
}

func (m *followUpTUIModel) moveCursor(delta int) {
	if m.pane == followUpScopesPane {
		if len(m.scopes) == 0 {
			return
		}
		m.scopeCursor = (m.scopeCursor + delta + len(m.scopes)) % len(m.scopes)
		m.stepCursor = 0
		return
	}
	steps := m.workflowSteps()
	if len(steps) == 0 {
		return
	}
	m.stepCursor = (m.stepCursor + delta + len(steps)) % len(steps)
}

func (m *followUpTUIModel) openFollowUpForm(edit bool, entry config.FollowUpConfig) {
	m.overlay = followUpFormOverlay
	m.formError = ""
	m.formEditName = ""
	m.formContinue = entry.ContinueOnError
	if edit {
		m.formEditName = entry.Name
	}
	m.inputs[0].SetValue(entry.Name)
	m.inputs[1].SetValue(entry.Run)
	m.inputs[2].SetValue(entry.Dir)
	m.formField = 0
	m.focusFormField()
}

func (m *followUpTUIModel) focusFormField() {
	for i := range m.inputs {
		m.inputs[i].Blur()
	}
	if m.formField < len(m.inputs) {
		m.inputs[m.formField].Focus()
	}
}

func (m *followUpTUIModel) moveFormField(delta int) {
	m.formField = (m.formField + delta + 4) % 4
	m.focusFormField()
}

func (m *followUpTUIModel) updateOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case followUpHelpOverlay:
		switch msg.String() {
		case "esc", "?", "enter":
			m.overlay = followUpNoOverlay
		}
		return m, nil
	case followUpDeleteOverlay:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc", "n":
			m.overlay = followUpNoOverlay
			m.pendingDelete = ""
		case "y", "enter":
			return m, m.deletePendingFollowUp()
		}
		return m, nil
	case followUpFormOverlay:
		return m.updateForm(msg)
	default:
		return m, nil
	}
}

func (m *followUpTUIModel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.closeOverlay()
		return m, nil
	case "tab", "down":
		m.moveFormField(1)
		return m, nil
	case "shift+tab", "up":
		m.moveFormField(-1)
		return m, nil
	case " ":
		if m.formField == 3 {
			m.formContinue = !m.formContinue
			return m, nil
		}
	case "enter":
		if m.formField < 3 {
			m.moveFormField(1)
			return m, nil
		}
		return m, m.saveForm()
	}
	if m.formField >= len(m.inputs) {
		return m, nil
	}
	var cmd tea.Cmd
	m.inputs[m.formField], cmd = m.inputs[m.formField].Update(msg)
	return m, cmd
}

func (m *followUpTUIModel) closeOverlay() {
	m.overlay = followUpNoOverlay
	m.formError = ""
	for i := range m.inputs {
		m.inputs[i].Blur()
	}
}

func (m *followUpTUIModel) saveForm() tea.Cmd {
	entry := config.FollowUpConfig{
		Name:            strings.TrimSpace(m.inputs[0].Value()),
		Run:             strings.TrimSpace(m.inputs[1].Value()),
		Dir:             strings.TrimSpace(m.inputs[2].Value()),
		ContinueOnError: m.formContinue,
	}
	if err := entry.Validate(); err != nil {
		m.formError = err.Error()
		return nil
	}

	entries := append([]config.FollowUpConfig(nil), m.effectiveFollowUps()...)
	if m.formEditName != "" {
		found := false
		for i := range entries {
			if entries[i].Name == m.formEditName {
				entries[i] = entry
				found = true
				break
			}
		}
		if !found {
			m.formError = fmt.Sprintf("follow-up %q is no longer configured; reload and try again", m.formEditName)
			return nil
		}
	} else {
		entries = append(entries, entry)
	}

	if err := config.SetFreshFollowUps(m.ctx, m.root, scopeProjectName(m.selectedScope()), entries); err != nil {
		m.formError = err.Error()
		return nil
	}
	selected := m.selectedScope()
	if err := m.refresh(selected); err != nil {
		m.formError = err.Error()
		return nil
	}
	m.closeOverlay()
	if m.formEditName != "" {
		m.setStatus(fmt.Sprintf("updated %q", entry.Name))
	} else {
		m.setStatus(fmt.Sprintf("added %q to %s", entry.Name, workflowScopeLabel(scopeProjectName(selected))))
	}
	return nil
}

func (m *followUpTUIModel) deletePendingFollowUp() tea.Cmd {
	entries := make([]config.FollowUpConfig, 0, len(m.effectiveFollowUps()))
	for _, entry := range m.effectiveFollowUps() {
		if entry.Name != m.pendingDelete {
			entries = append(entries, entry)
		}
	}
	name := m.pendingDelete
	selected := m.selectedScope()
	if err := config.SetFreshFollowUps(m.ctx, m.root, scopeProjectName(selected), entries); err != nil {
		m.setError(err)
		m.pendingDelete = ""
		m.overlay = followUpNoOverlay
		return nil
	}
	if err := m.refresh(selected); err != nil {
		m.setError(err)
		m.pendingDelete = ""
		m.overlay = followUpNoOverlay
		return nil
	}
	m.pendingDelete = ""
	m.overlay = followUpNoOverlay
	m.stepCursor = min(m.stepCursor, max(len(m.workflowSteps())-1, 0))
	m.setStatus(fmt.Sprintf("removed %q", name))
	return nil
}
