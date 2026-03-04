package explorer

import (
	"fmt"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/gather"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	tea "github.com/charmbracelet/bubbletea"
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
		sourceIDs := m.gatherDialog.IntentIDs()
		sourcePaths := make([]string, 0, len(sourceIDs))
		sourceStatusByID := make(map[string]intent.Status, len(sourceIDs))
		for _, id := range sourceIDs {
			src, getErr := m.service.Get(m.ctx, id)
			if getErr == nil && src.Path != "" {
				sourcePaths = append(sourcePaths, src.Path)
				sourceStatusByID[id] = src.Status
			}
		}

		result, err := svc.Gather(m.ctx, sourceIDs, opts)
		if err != nil {
			return gatherFinishedMsg{err: err}
		}

		if err := m.appendAuditEvent(audit.Event{
			Type:  audit.EventGather,
			ID:    result.Gathered.ID,
			Title: result.Gathered.Title,
			To:    string(result.Gathered.Status),
		}); err != nil {
			return gatherFinishedMsg{err: err}
		}

		if opts.ArchiveSources && len(result.ArchivedSources) > 0 {
			reason := gather.ArchiveReason(result.Gathered.ID, result.Gathered.Title)
			for _, archived := range result.ArchivedSources {
				if err := m.appendAuditEvent(audit.Event{
					Type:   audit.EventArchive,
					ID:     archived.ID,
					Title:  archived.Title,
					From:   string(sourceStatusByID[archived.ID]),
					To:     string(intent.StatusArchived),
					Reason: reason,
				}); err != nil {
					return gatherFinishedMsg{err: err}
				}
			}
		}

		// Auto-commit the gather operation using shared helper
		if m.campaignRoot != "" && m.campaignID != "" {
			description := fmt.Sprintf("Unified %d intents into %q\nSources: %v",
				result.SourceCount, opts.Title, sourceIDs)
			if len(result.ArchivedPaths) > 0 {
				description += fmt.Sprintf("\nArchived: %d source intents", len(result.ArchivedPaths))
			}

			files := make([]string, 0, len(sourcePaths)+len(result.ArchivedPaths)+1)
			files = append(files, sourcePaths...)
			if result.Gathered != nil && result.Gathered.Path != "" {
				files = append(files, result.Gathered.Path)
			}
			files = append(files, result.ArchivedPaths...)

			_ = commit.Intent(m.ctx, commit.IntentOptions{
				Options: commit.Options{
					CampaignRoot:  m.campaignRoot,
					CampaignID:    m.campaignID,
					Files:         commit.NormalizeFiles(m.campaignRoot, files...),
					SelectiveOnly: true,
				},
				Action:      commit.IntentGather,
				IntentTitle: opts.Title,
				Description: description,
			})
		}

		return gatherFinishedMsg{
			gatheredID:    result.Gathered.ID,
			gatheredTitle: result.Gathered.Title,
			sourceCount:   result.SourceCount,
		}
	}
}

// handleGatherStart opens gather dialog if 2+ intents are selected.
// Requires explicit selection — never auto-selects intents.
func (m *Model) handleGatherStart() (tea.Model, tea.Cmd) {
	if len(m.selectedIntents) >= 2 {
		intents := m.getSelectedIntentObjects()
		m.gatherDialog = tui.NewGatherDialog(intents)
		m.focus = focusGatherDialog
	} else {
		m.statusMessage = "Select 2+ intents with Space, then ga to gather"
	}
	return m, nil
}

// handleGatherGroup gathers all intents in the current status group.
func (m *Model) handleGatherGroup() (tea.Model, tea.Cmd) {
	if m.cursorGroup < 0 || m.cursorGroup >= len(m.groups) {
		return m, nil
	}
	group := m.groups[m.cursorGroup]
	if len(group.Intents) < 2 {
		m.statusMessage = "Group needs 2+ intents to gather"
		return m, nil
	}
	for _, i := range group.Intents {
		m.selectedIntents[i.ID] = true
	}
	m.multiSelectMode = true
	m.gatherDialog = tui.NewGatherDialog(group.Intents)
	m.focus = focusGatherDialog
	return m, nil
}

// enterGatherModeFromAction enters multi-select mode with current intent pre-selected.
func (m *Model) enterGatherModeFromAction(selected *intent.Intent) {
	m.multiSelectMode = true
	m.selectedIntents[selected.ID] = true
	m.statusMessage = "Select more intents with Space, then ga to gather"
}
