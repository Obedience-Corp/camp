package explorer

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
)

// convertTypeOptions are the intent types a note can be converted into.
var convertTypeOptions = []struct {
	name string
	typ  intent.Type
}{
	{"Idea", intent.TypeIdea},
	{"Feature", intent.TypeFeature},
	{"Bug", intent.TypeBug},
	{"Research", intent.TypeResearch},
	{"Chore", intent.TypeChore},
}

// groupNotes organizes notes into Notes and Archived groups for the notes view.
func groupNotes(notes []*intent.Intent) []IntentGroup {
	groups := []IntentGroup{
		{Name: "Notes", Status: intent.StatusNote, Expanded: true},
		{Name: "Archived", Status: intent.StatusNoteArchived, Expanded: true},
	}
	for _, n := range notes {
		idx := 0
		if n.Status == intent.StatusNoteArchived {
			idx = 1
		}
		groups[idx].Intents = append(groups[idx].Intents, n)
	}
	return groups
}

// toggleNotesMode switches between the intent triage view and the notes view,
// resets the cursor, and reloads the list.
func (m *Model) toggleNotesMode() tea.Cmd {
	m.notesMode = !m.notesMode
	m.cursorGroup = 0
	m.cursorItem = -1
	if m.notesMode {
		m.statusMessage = "Notes view"
	} else {
		m.statusMessage = "Intents view"
	}
	return m.loadIntents()
}

// startConvert opens the type picker to convert the selected note into an intent.
func (m *Model) startConvert() {
	selected := m.SelectedIntent()
	if selected == nil {
		return
	}
	m.startConvertToStatus(selected, intent.StatusInbox)
}

func (m *Model) startConvertToStatus(selected *intent.Intent, targetStatus intent.Status) {
	if selected == nil {
		return
	}
	m.noteToConvert = selected
	m.convertTypeIdx = 0
	m.convertTargetStatus = targetStatus
	m.focus = focusConvertType
}

// updateConvert handles key input while picking a type for note conversion.
func (m *Model) updateConvert(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusList
		m.noteToConvert = nil
		m.convertTargetStatus = ""
		return m, nil
	case "j", "down":
		if m.convertTypeIdx < len(convertTypeOptions)-1 {
			m.convertTypeIdx++
		}
	case "k", "up":
		if m.convertTypeIdx > 0 {
			m.convertTypeIdx--
		}
	case "enter":
		if m.noteToConvert != nil {
			note := m.noteToConvert
			newType := convertTypeOptions[m.convertTypeIdx].typ
			targetStatus := m.convertTargetStatus
			if targetStatus == "" {
				targetStatus = intent.StatusInbox
			}
			m.focus = focusList
			m.noteToConvert = nil
			m.convertTargetStatus = ""
			return m, m.convertNote(note, newType, targetStatus)
		}
	}
	return m, nil
}

// convertNote runs the conversion and emits a moveFinishedMsg so the list reloads.
func (m *Model) convertNote(note *intent.Intent, newType intent.Type, targetStatus intent.Status) tea.Cmd {
	return func() tea.Msg {
		sourcePath := note.Path
		converted, err := m.service.MoveNoteToStatus(m.ctx, note.ID, targetStatus, newType)
		if err == nil {
			err = m.appendAuditEvent(audit.Event{
				Type:   audit.EventMove,
				ID:     note.ID,
				Title:  note.Title,
				From:   string(intent.StatusNote),
				To:     string(targetStatus),
				Reason: "converted note to " + string(newType),
			})
		}
		if err == nil {
			m.autoCommitIntent(commit.IntentMove, note.Title, "Converted note to "+string(newType)+" intent", sourcePath, converted.Path)
		}
		return moveFinishedMsg{
			err:       err,
			intentID:  note.ID,
			newStatus: targetStatus,
		}
	}
}

// archiveNote moves the note into notes/archived/ and emits an
// archiveFinishedMsg so the list reloads. Notes carry no lifecycle metadata or
// decision records, so archiving is reason-free, unlike dungeoning an intent.
func (m *Model) archiveNote(note *intent.Intent) tea.Cmd {
	return func() tea.Msg {
		sourcePath := note.Path
		archived, err := m.service.ArchiveNote(m.ctx, note.ID)
		if err == nil {
			err = m.appendAuditEvent(audit.Event{
				Type:  audit.EventArchive,
				ID:    note.ID,
				Title: note.Title,
				From:  string(intent.StatusNote),
				To:    string(intent.StatusNoteArchived),
			})
		}
		if err == nil {
			m.autoCommitIntent(commit.IntentArchive, note.Title, "Archived note", sourcePath, archived.Path)
		}
		return archiveFinishedMsg{err: err, intentID: note.ID}
	}
}

// restoreNote moves an archived note back into the active notes/ store and emits
// a moveFinishedMsg so the list reloads.
func (m *Model) restoreNote(note *intent.Intent) tea.Cmd {
	return func() tea.Msg {
		sourcePath := note.Path
		restored, err := m.service.RestoreNote(m.ctx, note.ID)
		if err == nil {
			err = m.appendAuditEvent(audit.Event{
				Type:  audit.EventMove,
				ID:    note.ID,
				Title: note.Title,
				From:  string(intent.StatusNoteArchived),
				To:    string(intent.StatusNote),
			})
		}
		if err == nil {
			m.autoCommitIntent(commit.IntentMove, note.Title, "Restored note", sourcePath, restored.Path)
		}
		return moveFinishedMsg{err: err, intentID: note.ID, newStatus: intent.StatusNote}
	}
}

// viewConvert renders the convert type picker.
func (m *Model) viewConvert() string {
	var b strings.Builder

	title := "Convert Note to Intent"
	if m.convertTargetStatus != "" && m.convertTargetStatus != intent.StatusInbox {
		title = "Move Note to " + m.convertTargetStatus.String()
	}
	b.WriteString(tui.TitleStyle.Render(title))
	b.WriteString("\n\n")

	if m.noteToConvert != nil {
		b.WriteString("Note: " + m.noteToConvert.Title + "\n\n")
	}

	if m.convertTargetStatus != "" {
		b.WriteString("Moving to: " + m.convertTargetStatus.String() + "\n\n")
	}

	b.WriteString("Select intent type:\n")
	for i, opt := range convertTypeOptions {
		cursor := "  "
		if i == m.convertTypeIdx {
			cursor = "> "
		}
		b.WriteString(cursor + opt.name + "\n")
	}
	b.WriteString("\n")
	b.WriteString(tui.HelpStyle.Render("j/k: navigate . Enter: convert . Esc: cancel"))

	return b.String()
}
