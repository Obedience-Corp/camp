package explorer

import (
	tea "github.com/charmbracelet/bubbletea"

	campui "github.com/Obedience-Corp/camp/internal/ui"
)

// handleCopyID copies the selected intent's or note's frontmatter id to the
// operator's clipboard. Notes carry ids in the same shape as lifecycle
// intents, so one action covers both views.
func (m Model) handleCopyID() (tea.Model, tea.Cmd) {
	selected := m.SelectedIntent()
	if selected == nil {
		m.setStatus("No intent selected — move the cursor onto an item to copy its id")
		return m, nil
	}
	if selected.ID == "" {
		m.setStatusError("Intent has no id to copy")
		return m, nil
	}

	if err := campui.WriteClipboard(selected.ID); err != nil {
		m.setStatusError("Copy failed: " + err.Error())
		return m, nil
	}

	m.setStatusSuccess("Copied " + selected.ID)
	return m, nil
}
