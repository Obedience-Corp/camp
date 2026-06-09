package explorer

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
)

// tagsUpdatedMsg signals that a tag edit finished.
type tagsUpdatedMsg struct {
	err error
}

// startTagEdit opens the tag overlay for the selected intent or note.
func (m *Model) startTagEdit() {
	selected := m.SelectedIntent()
	if selected == nil {
		return
	}
	m.tagTarget = selected
	m.tagOverlay = tui.NewTagOverlay(m.availableTags, selected.Tags)
	m.tagging = true
}

// updateTagEdit routes keys to the overlay and persists on confirm.
func (m *Model) updateTagEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var done bool
	m.tagOverlay, done = m.tagOverlay.Update(msg)
	if !done {
		return m, nil
	}

	m.tagging = false
	if m.tagOverlay.Cancelled() || m.tagTarget == nil {
		m.tagTarget = nil
		return m, nil
	}

	target := m.tagTarget
	tags := m.tagOverlay.Result()
	m.tagTarget = nil
	return m, m.persistTags(target, tags)
}

// persistTags writes the new tag set. Notes are saved directly (they live
// outside the intent lookup); intents go through UpdateDirect for auditing.
func (m *Model) persistTags(target *intent.Intent, tags []string) tea.Cmd {
	return func() tea.Msg {
		if target.Status.IsNote() {
			note, err := m.service.GetNote(m.ctx, target.ID)
			if err != nil {
				return tagsUpdatedMsg{err: err}
			}
			note.Tags = tags
			if err := m.service.Save(m.ctx, note); err != nil {
				return tagsUpdatedMsg{err: err}
			}
			return tagsUpdatedMsg{}
		}

		updated, changes, err := m.service.UpdateDirect(m.ctx, target.ID, intent.UpdateOptions{Tags: &tags})
		if err != nil {
			return tagsUpdatedMsg{err: err}
		}
		if len(changes) > 0 {
			_ = m.appendAuditEvent(audit.Event{Type: audit.EventEdit, ID: updated.ID, Title: updated.Title, Changes: changes})
			m.autoCommitIntent(commit.IntentEdit, updated.Title, "Updated tags", updated.Path)
		}
		return tagsUpdatedMsg{}
	}
}

// viewTagEdit renders the tag overlay with the target's title as a header.
func (m *Model) viewTagEdit() string {
	header := ""
	if m.tagTarget != nil {
		header = tui.HelpStyle.Render("Editing tags: ") + m.tagTarget.Title + "\n\n"
	}
	return header + m.tagOverlay.View()
}
