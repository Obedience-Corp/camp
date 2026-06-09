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
	m.noteToConvert = selected
	m.convertTypeIdx = 0
	m.focus = focusConvertType
}

// updateConvert handles key input while picking a type for note conversion.
func (m *Model) updateConvert(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusList
		m.noteToConvert = nil
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
			m.focus = focusList
			m.noteToConvert = nil
			return m, m.convertNote(note, newType)
		}
	}
	return m, nil
}

// convertNote runs the conversion and emits a moveFinishedMsg so the list reloads.
func (m *Model) convertNote(note *intent.Intent, newType intent.Type) tea.Cmd {
	return func() tea.Msg {
		sourcePath := note.Path
		converted, err := m.service.Convert(m.ctx, note.ID, newType)
		if err == nil {
			err = m.appendAuditEvent(audit.Event{
				Type:   audit.EventMove,
				ID:     note.ID,
				Title:  note.Title,
				From:   string(intent.StatusNote),
				To:     string(intent.StatusInbox),
				Reason: "converted note to " + string(newType),
			})
		}
		if err == nil {
			m.autoCommitIntent(commit.IntentMove, note.Title, "Converted note to "+string(newType)+" intent", sourcePath, converted.Path)
		}
		return moveFinishedMsg{
			err:       err,
			intentID:  note.ID,
			newStatus: intent.StatusInbox,
		}
	}
}

// viewConvert renders the convert type picker.
func (m *Model) viewConvert() string {
	var b strings.Builder

	b.WriteString(tui.TitleStyle.Render("Convert Note to Intent"))
	b.WriteString("\n\n")

	if m.noteToConvert != nil {
		b.WriteString("Note: " + m.noteToConvert.Title + "\n\n")
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
