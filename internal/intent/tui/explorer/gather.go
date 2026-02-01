package explorer

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/obediencecorp/camp/internal/git"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/intent/gather"
	"github.com/obediencecorp/camp/internal/intent/tui"
)

// toggleSelection toggles the selection state of an intent for multi-select gather.
func (m *Model) toggleSelection(i *intent.Intent) {
	if i == nil {
		return
	}

	if m.selectedIntents[i.ID] {
		delete(m.selectedIntents, i.ID)
		// Exit multi-select mode if no selections remain
		if len(m.selectedIntents) == 0 {
			m.multiSelectMode = false
		}
	} else {
		m.selectedIntents[i.ID] = true
		m.multiSelectMode = true
	}
}

// exitMultiSelectMode clears all selections and exits multi-select mode.
func (m *Model) exitMultiSelectMode() {
	m.selectedIntents = make(map[string]bool)
	m.multiSelectMode = false
	m.statusMessage = "Selection cleared"
}

// getSelectedIntentObjects returns the full Intent objects for all selected IDs.
func (m *Model) getSelectedIntentObjects() []*intent.Intent {
	var intents []*intent.Intent
	for _, i := range m.intents {
		if m.selectedIntents[i.ID] {
			intents = append(intents, i)
		}
	}
	return intents
}

// getSelectedIDs returns the IDs of all selected intents.
func (m *Model) getSelectedIDs() []string {
	ids := make([]string, 0, len(m.selectedIntents))
	for id := range m.selectedIntents {
		ids = append(ids, id)
	}
	return ids
}

// executeGather runs the gather operation using the gather service.
func (m *Model) executeGather() tea.Cmd {
	return func() tea.Msg {
		svc := gather.NewService(m.service, m.intentsDir)
		opts := gather.GatherOptions{
			Title:          m.gatherDialog.Title(),
			ArchiveSources: m.gatherDialog.ArchiveSources(),
		}
		result, err := svc.Gather(m.ctx, m.gatherDialog.IntentIDs(), opts)
		if err != nil {
			return gatherFinishedMsg{err: err}
		}

		// Auto-commit the gather operation
		if m.campaignRoot != "" && m.campaignID != "" {
			shortID := m.campaignID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}

			sourceIDs := m.gatherDialog.IntentIDs()
			commitMsg := fmt.Sprintf("[OBEY-CAMPAIGN-%s] Gather: %s\n\nUnified %d intents into %q\nSources: %s",
				shortID,
				opts.Title,
				result.SourceCount,
				opts.Title,
				strings.Join(sourceIDs, ", "),
			)
			if len(result.ArchivedPaths) > 0 {
				commitMsg += fmt.Sprintf("\nArchived: %d source intents", len(result.ArchivedPaths))
			}

			// CommitAll has built-in lock handling with retry
			// Ignore errors - don't fail gather just because commit failed
			_ = git.CommitAll(m.ctx, m.campaignRoot, commitMsg)
		}

		return gatherFinishedMsg{
			gatheredID:    result.Gathered.ID,
			gatheredTitle: result.Gathered.Title,
			sourceCount:   result.SourceCount,
		}
	}
}

// handleGatherStart opens gather dialog if 2+ intents are selected.
func (m *Model) handleGatherStart() (tea.Model, tea.Cmd) {
	if len(m.selectedIntents) >= 2 {
		intents := m.getSelectedIntentObjects()
		m.gatherDialog = tui.NewGatherDialog(intents)
		m.focus = focusGatherDialog
	} else if len(m.selectedIntents) == 1 {
		m.statusMessage = "Select at least 2 intents to gather (Space to select)"
	} else {
		m.statusMessage = "Select intents first (Space to select, then Ctrl-g to gather)"
	}
	return m, nil
}

// enterGatherModeFromAction enters multi-select mode with current intent pre-selected.
func (m *Model) enterGatherModeFromAction(selected *intent.Intent) {
	m.multiSelectMode = true
	m.selectedIntents[selected.ID] = true
	m.statusMessage = "Select more intents with Space, then Ctrl-g to gather"
}
