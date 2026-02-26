package tui

import (
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/gather"

	tea "github.com/charmbracelet/bubbletea"
)

// ViewerSimilarFoundMsg is sent when similar intents are found.
type ViewerSimilarFoundMsg struct {
	Similar []gather.SimilarResult
	Err     error
}

// ViewerGatherFinishedMsg is sent when gather operation completes from viewer.
type ViewerGatherFinishedMsg struct {
	GatheredID    string
	GatheredTitle string
	SourceCount   int
	Err           error
}

// findSimilarIntents searches for intents similar to the current one.
// Rebuilds the index each time to reflect any status changes during the session.
func (m IntentViewerModel) findSimilarIntents() tea.Cmd {
	return func() tea.Msg {
		if err := m.gatherSvc.BuildIndex(m.ctx); err != nil {
			return ViewerSimilarFoundMsg{Err: err}
		}
		// Use lower threshold (0.15) since composite similarity includes
		// metadata matching which produces lower scores than pure TF-IDF
		similar, err := m.gatherSvc.FindSimilar(m.ctx, m.intent.ID, 0.15)
		return ViewerSimilarFoundMsg{Similar: similar, Err: err}
	}
}

// getSelectedSimilarIntents returns the full Intent objects for selected similar intents.
func (m *IntentViewerModel) getSelectedSimilarIntents() []*intent.Intent {
	var intents []*intent.Intent
	for _, sim := range m.similarIntents {
		if m.selectedSimilar[sim.Intent.ID] {
			intents = append(intents, sim.Intent)
		}
	}
	return intents
}

// executeViewerGather runs the gather operation with current + selected similar intents.
func (m IntentViewerModel) executeViewerGather() tea.Cmd {
	return func() tea.Msg {
		opts := gather.GatherOptions{
			Title:          m.gatherDialog.Title(),
			ArchiveSources: m.gatherDialog.ArchiveSources(),
		}
		result, err := m.gatherSvc.Gather(m.ctx, m.gatherDialog.IntentIDs(), opts)
		if err != nil {
			return ViewerGatherFinishedMsg{Err: err}
		}
		return ViewerGatherFinishedMsg{
			GatheredID:    result.Gathered.ID,
			GatheredTitle: result.Gathered.Title,
			SourceCount:   result.SourceCount,
		}
	}
}
