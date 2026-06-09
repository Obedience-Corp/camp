package explorer

import (
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// addTUIFinishedMsg signals that the embedded add TUI has finished.
// This replaces tea.QuitMsg so the explorer stays alive.
type addTUIFinishedMsg struct{}

// startAddTUI launches the full IntentAddModel within the explorer. In the
// notes view it launches the note quick-add (text only, no type/concept).
func (m *Model) startAddTUI() {
	addModel := tui.NewIntentAddModel(m.ctx, m.conceptSvc, tui.AddOptions{
		FullMode:     !m.notesMode,
		NoteMode:     m.notesMode,
		Author:       m.author,
		CampaignRoot: m.campaignRoot,
		Shortcuts:    m.shortcuts,
	})
	m.addModel = &addModel
	m.focus = focusAddTUI
}

// updateAddTUI forwards messages to the embedded add model and checks completion.
func (m *Model) updateAddTUI(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.addModel == nil {
		m.focus = focusList
		return m, nil
	}

	updated, cmd := m.addModel.Update(msg)
	updatedModel := updated.(tui.IntentAddModel)
	m.addModel = &updatedModel

	if m.addModel.Done() {
		return m.finishAddTUI()
	}

	// Intercept tea.Quit commands so the explorer stays alive
	if cmd != nil {
		cmd = filterQuitCmd(cmd)
	}

	return m, cmd
}

// finishAddTUI processes results from the add model and returns to the list.
func (m *Model) finishAddTUI() (tea.Model, tea.Cmd) {
	// Process rapid-fire saved results (Ctrl-N)
	for _, saved := range m.addModel.SavedResults() {
		m.createIntentFromAddResult(saved)
	}

	// Process the final result (normal save-and-quit)
	if result := m.addModel.Result(); result != nil {
		m.createIntentFromAddResult(result)
	}

	m.addModel = nil
	m.focus = focusList

	return m, m.loadIntents()
}

// createIntentFromAddResult creates an intent (or a note, in notes mode) and
// auto-commits it.
func (m *Model) createIntentFromAddResult(result *tui.AddResult) {
	opts := intent.CreateOptions{
		Title:   result.Title,
		Type:    intent.Type(result.Type),
		Concept: result.Concept,
		Body:    result.Body,
		Author:  result.Author,
	}

	noun := "Intent"
	create := m.service.CreateDirect
	if m.notesMode {
		noun = "Note"
		create = m.service.CreateNote
	}
	created, err := create(m.ctx, opts)
	if err != nil {
		m.statusMessage = "Error creating " + noun + ": " + err.Error()
		return
	}

	if err := m.appendAuditEvent(audit.Event{
		Type:  audit.EventCreate,
		ID:    created.ID,
		Title: created.Title,
		To:    string(created.Status),
	}); err != nil {
		m.statusMessage = "Error writing audit event: " + err.Error()
		return
	}

	// Auto-commit
	if m.campaignRoot != "" && m.campaignID != "" {
		m.autoCommitIntent(commit.IntentCreate, result.Title, "", created.Path)
	}

	m.statusMessage = noun + " created: " + result.Title
}

// filterQuitCmd wraps a tea.Cmd to intercept tea.QuitMsg and convert it
// to addTUIFinishedMsg, preventing the explorer from exiting.
func filterQuitCmd(cmd tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		msg := cmd()
		if _, ok := msg.(tea.QuitMsg); ok {
			return addTUIFinishedMsg{}
		}
		return msg
	}
}
