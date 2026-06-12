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

// startAddTUI launches the full IntentAddModel within the explorer. From the
// notes group/view it launches note capture (title/body/tags, no type/concept).
func (m *Model) startAddTUI() {
	noteMode := m.shouldCreateNoteFromCurrentPosition()
	addModel := tui.NewIntentAddModel(m.ctx, m.conceptSvc, tui.AddOptions{
		FullMode:      !noteMode,
		NoteMode:      noteMode,
		Author:        m.author,
		CampaignRoot:  m.campaignRoot,
		Shortcuts:     m.shortcuts,
		AvailableTags: m.availableTags,
	})
	m.addModel = &addModel
	m.addNoteMode = noteMode
	m.focus = focusAddTUI
}

func (m *Model) shouldCreateNoteFromCurrentPosition() bool {
	if m.notesMode {
		return true
	}
	if m.cursorGroup < 0 || m.cursorGroup >= len(m.groups) {
		return false
	}
	group := m.groups[m.cursorGroup]
	return group.Status.IsNote()
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
	m.addNoteMode = false
	m.focus = focusList

	return m, m.loadIntents()
}

// createIntentFromAddResult creates an intent (or a note, in add note mode) and
// auto-commits it.
func (m *Model) createIntentFromAddResult(result *tui.AddResult) {
	opts := intent.CreateOptions{
		Title:   result.Title,
		Type:    intent.Type(result.Type),
		Concept: result.Concept,
		Body:    result.Body,
		Author:  result.Author,
		Tags:    result.Tags,
	}

	noun := "Intent"
	create := m.service.CreateDirect
	if m.addNoteMode {
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
