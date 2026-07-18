package fresh

import (
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui"
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
	case "shift+up", "K":
		return m, m.moveSelectedFollowUp(-1)
	case "shift+down", "J":
		return m, m.moveSelectedFollowUp(1)
	case "left", "h":
		m.pane = followUpScopesPane
	case "right", "l", "tab":
		m.pane = followUpWorkflowPane
	case "shift+tab":
		m.pane = followUpScopesPane
	case "enter":
		if m.pane == followUpScopesPane {
			m.pane = followUpWorkflowPane
			return m, nil
		}
		return m.activateSelectedStep()
	case "a":
		m.openFollowUpForm(false, config.FollowUpConfig{})
		return m, textinput.Blink
	case "e":
		if _, entry, ok := m.selectedFollowUp(); ok {
			m.openFollowUpForm(true, entry)
			return m, textinput.Blink
		}
		// e and enter mean the same thing on a settings row, so a user who
		// learned "e edits" from the follow-ups section is not bounced off the
		// section above it.
		return m.activateSelectedStep()
	case "d":
		if _, entry, ok := m.selectedFollowUp(); ok {
			m.pendingDelete = entry.Name
			m.overlay = followUpDeleteOverlay
			return m, nil
		}
		m.noticeForUnsupportedVerb("delete")
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

// activateSelectedStep opens the editor that fits the row under the cursor.
// Rows with nothing to configure explain themselves rather than failing: the
// pane already groups them under a header that says so, and a red error for
// pressing enter on "Pull" would be noise.
func (m *followUpTUIModel) activateSelectedStep() (tea.Model, tea.Cmd) {
	step, ok := m.selectedStep()
	if !ok {
		return m, nil
	}

	switch {
	case step.Kind == freshStepFollowUp:
		if _, entry, found := m.selectedFollowUp(); found {
			m.openFollowUpForm(true, entry)
			return m, textinput.Blink
		}
		return m, nil
	case step.Kind == freshStepSetting && step.GlobalOnly && m.inProjectScope():
		// Writing the key under this project would produce a fresh.yaml that
		// looks configured and changes nothing, because ResolveFreshPrune never
		// consults a project. Send the user where the key actually lives.
		m.setNotice(fmt.Sprintf("%s is a campaign-wide setting · select Global defaults to change it",
			settingTitle(step.Setting)))
		return m, nil
	case step.Kind == freshStepSetting:
		m.openSettingEditor(step)
		return m, textinput.Blink
	default:
		m.setNotice(fmt.Sprintf("%s always runs · nothing to configure", step.Title))
		return m, nil
	}
}

// noticeForUnsupportedVerb explains why a follow-up verb did nothing on the
// selected row, naming the row rather than restating the requirement.
func (m *followUpTUIModel) noticeForUnsupportedVerb(verb string) {
	step, ok := m.selectedStep()
	if !ok {
		m.setNotice("select a follow-up step to " + verb)
		return
	}
	if step.Kind == freshStepSetting {
		m.setNotice(fmt.Sprintf("%q is a fresh setting · press enter to change it, not %s", step.Title, verb))
		return
	}
	m.setNotice(fmt.Sprintf("%q is a built-in step · only follow-ups can be %sd", step.Title, verb))
}

func (m *followUpTUIModel) openSettingEditor(step freshWorkflowStep) {
	m.overlay = followUpSettingOverlay
	m.settingStep = step
	m.settingError = ""
	m.settingOptions = m.settingOptionsFor(step)

	current := m.currentSettingAction(step)
	m.settingChoice = 0
	for i, option := range m.settingOptions {
		if option.action == current {
			m.settingChoice = i
			break
		}
	}

	m.settingInput.SetValue(m.settingScopeBranch())
	m.settingInput.Blur()
	if step.Setting == freshSettingBranch && current == freshSettingCustomBranch {
		m.settingInput.Focus()
	}
}

func (m *followUpTUIModel) moveSelectedFollowUp(delta int) tea.Cmd {
	_, entry, ok := m.selectedFollowUp()
	if !ok {
		m.noticeForUnsupportedVerb("move")
		return nil
	}

	forked := m.scopeInheritsGlobal()
	ordered, err := config.ReorderFreshFollowUps(m.effectiveFollowUps(), entry.Name, delta)
	if err != nil {
		m.setError(err)
		return nil
	}

	scope := m.selectedScope()
	if err := config.SetFreshFollowUps(m.ctx, m.root, scopeProjectName(scope), ordered); err != nil {
		m.setError(err)
		return nil
	}
	if err := m.refresh(scope); err != nil {
		m.setError(err)
		return nil
	}

	for i, step := range m.workflowSteps() {
		if step.Follow != nil && step.Follow.Name == entry.Name {
			m.stepCursor = i
			break
		}
	}
	direction := "down"
	if delta < 0 {
		direction = "up"
	}
	status := fmt.Sprintf("moved %q %s in %s", entry.Name, direction, workflowScopeLabel(scopeProjectName(scope)))
	if forked {
		status += fmt.Sprintf(" · now overrides the global list with %s",
			ui.CountLabel(len(ordered), "step", "steps"))
	}
	m.setStatus(status)
	return nil
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
	case followUpSettingOverlay:
		return m.updateSettingEditor(msg)
	default:
		return m, nil
	}
}

func (m *followUpTUIModel) updateSettingEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.closeSettingEditor()
		return m, nil
	case "up", "shift+tab":
		m.moveSettingChoice(-1)
		return m, nil
	case "down", "tab":
		m.moveSettingChoice(1)
		return m, nil
	case "enter":
		return m, m.saveSettingEditor()
	}

	// Typing only reaches the branch input, and only while the custom option is
	// the selected one. Anywhere else the keystroke would edit a value the user
	// cannot see, so it is dropped.
	if m.settingInput.Focused() {
		var cmd tea.Cmd
		m.settingInput, cmd = m.settingInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *followUpTUIModel) moveSettingChoice(delta int) {
	if len(m.settingOptions) == 0 {
		return
	}
	m.settingChoice = (m.settingChoice + delta + len(m.settingOptions)) % len(m.settingOptions)
	m.settingError = ""
	if m.selectedSettingAction() == freshSettingCustomBranch {
		m.settingInput.Focus()
		return
	}
	m.settingInput.Blur()
}

func (m *followUpTUIModel) selectedSettingAction() freshSettingAction {
	if m.settingChoice < 0 || m.settingChoice >= len(m.settingOptions) {
		return freshSettingInherit
	}
	return m.settingOptions[m.settingChoice].action
}

func (m *followUpTUIModel) closeSettingEditor() {
	m.overlay = followUpNoOverlay
	m.settingError = ""
	m.settingInput.Blur()
}

func (m *followUpTUIModel) saveSettingEditor() tea.Cmd {
	step := m.settingStep
	scope := m.selectedScope()
	projectName := scopeProjectName(scope)
	action := m.selectedSettingAction()

	var (
		err     error
		outcome string
	)
	switch step.Setting {
	case freshSettingBranch:
		branch, description, verr := m.resolveBranchAction(action)
		if verr != nil {
			m.settingError = verr.Error()
			return nil
		}
		outcome = description
		err = config.SetFreshBranch(m.ctx, m.root, projectName, branch)
	case freshSettingPushUpstream:
		value := boolForAction(action)
		outcome = settingOutcome("push_upstream", value)
		err = config.SetFreshPushUpstream(m.ctx, m.root, projectName, value)
	case freshSettingPrune:
		value := boolForAction(action)
		outcome = settingOutcome("prune", value)
		err = config.SetFreshPrune(m.ctx, m.root, value)
	case freshSettingPruneRemote:
		value := boolForAction(action)
		outcome = settingOutcome("prune_remote", value)
		err = config.SetFreshPruneRemote(m.ctx, m.root, value)
	default:
		m.closeSettingEditor()
		return nil
	}

	if err != nil {
		m.settingError = err.Error()
		return nil
	}
	if err := m.refresh(scope); err != nil {
		m.settingError = err.Error()
		return nil
	}

	m.closeSettingEditor()
	m.setStatus(fmt.Sprintf("%s in %s", outcome, workflowScopeLabel(projectName)))
	return nil
}

// resolveBranchAction turns the selected option into the branch value to
// write, rejecting a custom branch with no name rather than writing an empty
// string that would read back as "no branch".
func (m *followUpTUIModel) resolveBranchAction(action freshSettingAction) (*string, string, error) {
	switch action {
	case freshSettingInherit:
		return nil, "branch now inherits the global default", nil
	case freshSettingNoBranch:
		empty := ""
		return &empty, "branch cleared · fresh stays on the default branch", nil
	default:
		name := strings.TrimSpace(m.settingInput.Value())
		if name == "" {
			return nil, "", camperrors.NewValidation("branch", "must not be empty; choose \"no branch\" to stay on the default branch", nil)
		}
		return &name, fmt.Sprintf("branch set to %s", name), nil
	}
}

// boolForAction maps an option onto the pointer the config writers take, where
// nil clears the key.
func boolForAction(action freshSettingAction) *bool {
	switch action {
	case freshSettingOn:
		on := true
		return &on
	case freshSettingOff:
		off := false
		return &off
	default:
		return nil
	}
}

func settingOutcome(key string, value *bool) string {
	if value == nil {
		return key + " now inherits the global default"
	}
	return fmt.Sprintf("%s set to %s", key, onOffWord(*value))
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

// forkNotice explains, for a project scope with no list of its own, what
// saving will do to it. A project's follow-up list replaces the global one
// rather than extending it, so the first edit has to carry the global steps
// across or the project would silently lose them.
func (m *followUpTUIModel) forkNotice() string {
	if !m.scopeInheritsGlobal() {
		return ""
	}
	// The overlay title already names the project, and a long project name here
	// would push the line past the overlay's width and be clipped.
	inherited := len(m.cfg.FollowUp)
	if inherited == 0 {
		return "Saving creates a follow-up list for this project."
	}
	return fmt.Sprintf("Saving copies the %s into this project's own list.",
		ui.CountLabel(inherited, "global step", "global steps"))
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

	forked := m.scopeInheritsGlobal()
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
		// A name already in the resolved list would be written twice and
		// rejected on validation. Say which list it came from, because the
		// answer decides what the user does next: a clashing global step is
		// edited into a project-specific one rather than added alongside.
		for _, existing := range entries {
			if existing.Name != entry.Name {
				continue
			}
			if forked {
				m.formError = fmt.Sprintf("%q is inherited from global · press e to edit it here", entry.Name)
			} else {
				m.formError = fmt.Sprintf("%q already exists here · press e to edit it", entry.Name)
			}
			return nil
		}
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
	action := "added"
	if m.formEditName != "" {
		action = "updated"
	}
	status := fmt.Sprintf("%s %q in %s", action, entry.Name, workflowScopeLabel(scopeProjectName(selected)))
	if forked {
		status += fmt.Sprintf(" · now overrides the global list with %s",
			ui.CountLabel(len(entries), "step", "steps"))
	}
	m.setStatus(status)
	return nil
}

func (m *followUpTUIModel) deletePendingFollowUp() tea.Cmd {
	forked := m.scopeInheritsGlobal()
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
	status := fmt.Sprintf("removed %q from %s", name, workflowScopeLabel(scopeProjectName(selected)))
	if forked {
		status += fmt.Sprintf(" · now overrides the global list with %s",
			ui.CountLabel(len(entries), "step", "steps"))
	}
	m.setStatus(status)
	return nil
}
