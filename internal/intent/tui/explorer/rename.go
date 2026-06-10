package explorer

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
)

// renameFinishedMsg signals that a rename finished. renamedID carries the
// stable id so the list can reselect the renamed item.
type renameFinishedMsg struct {
	err       error
	renamedID string
}

// startRename opens the rename overlay prefilled with the selected item's title.
func (m *Model) startRename() {
	selected := m.SelectedIntent()
	if selected == nil {
		return
	}
	m.renameTarget = selected
	ti := textinput.New()
	ti.CharLimit = 100
	ti.Width = 60
	ti.SetValue(selected.Title)
	ti.CursorEnd()
	ti.Focus()
	m.renameInput = ti
	m.focus = focusRename
}

// updateRename handles keys while the rename input is active.
func (m *Model) updateRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.focus = focusList
		m.renameTarget = nil
		return m, nil
	case "enter":
		newTitle := strings.TrimSpace(m.renameInput.Value())
		target := m.renameTarget
		m.focus = focusList
		m.renameTarget = nil
		if target == nil || newTitle == "" || newTitle == target.Title {
			return m, nil
		}
		return m, m.renameIntent(target, newTitle)
	}
	var cmd tea.Cmd
	m.renameInput, cmd = m.renameInput.Update(msg)
	return m, cmd
}

// renameIntent runs the rename and emits a renameFinishedMsg.
func (m *Model) renameIntent(target *intent.Intent, newTitle string) tea.Cmd {
	return func() tea.Msg {
		oldTitle := target.Title
		oldPath := target.Path
		renamed, err := m.service.Rename(m.ctx, target.ID, newTitle)
		if err != nil {
			return renameFinishedMsg{err: err}
		}
		_ = m.appendAuditEvent(audit.Event{
			Type:  audit.EventRename,
			ID:    renamed.ID,
			Title: renamed.Title,
			Changes: []audit.FieldChange{
				{Field: "title", Old: oldTitle, New: renamed.Title},
			},
		})
		m.autoCommitIntent(commit.IntentRename, renamed.Title, "Renamed from "+oldTitle, oldPath, renamed.Path)
		return renameFinishedMsg{renamedID: renamed.ID}
	}
}

// viewRename renders the rename input overlay.
func (m *Model) viewRename() string {
	var b strings.Builder
	b.WriteString(tui.TitleStyle.Render("Rename Intent"))
	b.WriteString("\n\n")
	if m.renameTarget != nil {
		b.WriteString(tui.HelpStyle.Render("Current: ") + m.renameTarget.Title + "\n\n")
	}
	b.WriteString("New title:\n")
	b.WriteString(m.renameInput.View())
	b.WriteString("\n\n")
	b.WriteString(tui.HelpStyle.Render("Enter: rename . Esc: cancel"))
	return b.String()
}

// reselectByID moves the cursor to the item with the given id after a reload.
func (m *Model) reselectByID(id string) {
	for gi := range m.groups {
		for ii, it := range m.groups[gi].Intents {
			if it.ID == id {
				m.cursorGroup = gi
				m.cursorItem = ii
				return
			}
		}
	}
}
