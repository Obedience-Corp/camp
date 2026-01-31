package explorer

import (
	"strings"

	"github.com/obediencecorp/camp/internal/intent"
)

// typeFilterItems are the available type filter options.
var typeFilterItems = []string{"All", "Idea", "Feature", "Bug", "Research", "Chore"}

// statusFilterItems are the available status filter options.
var statusFilterItems = []string{"All", "Inbox", "Active", "Ready", "Done", "Killed"}

// applyFilters filters intents using search query and type filter.
func (m *Model) applyFilters() {
	query := m.searchInput.Value()
	m.statusMessage = ""

	// Start with all intents
	var filtered []*intent.Intent

	// Apply search if there's a query
	if query == "" {
		filtered = m.intents
	} else {
		// Use fuzzy search via the service
		results, err := m.service.Search(m.ctx, query)
		if err != nil {
			m.statusMessage = "Search error: " + err.Error()
			filtered = m.intents
		} else {
			filtered = results
		}
	}

	// Apply type filter
	typeSelection := m.typeWheel.SelectedValue()
	if typeSelection != "All" && typeSelection != "" {
		typeFiltered := make([]*intent.Intent, 0)
		targetType := strings.ToLower(typeSelection)
		for _, i := range filtered {
			if string(i.Type) == targetType {
				typeFiltered = append(typeFiltered, i)
			}
		}
		filtered = typeFiltered
	}

	// Apply status filter
	statusSelection := m.statusWheel.SelectedValue()
	if statusSelection != "All" && statusSelection != "" {
		statusFiltered := make([]*intent.Intent, 0)
		targetStatus := strings.ToLower(statusSelection)
		for _, i := range filtered {
			if string(i.Status) == targetStatus {
				statusFiltered = append(statusFiltered, i)
			}
		}
		filtered = statusFiltered
	}

	// Apply concept filter
	if m.conceptFilterPath != "" {
		conceptFiltered := make([]*intent.Intent, 0)
		for _, i := range filtered {
			// Match if intent's concept starts with the filter path
			if strings.HasPrefix(i.Concept, m.conceptFilterPath) {
				conceptFiltered = append(conceptFiltered, i)
			}
		}
		filtered = conceptFiltered
	}

	m.filteredIntents = filtered

	// Rebuild groups from filtered intents
	m.groups = groupIntentsByStatus(m.filteredIntents)

	// Reset cursor position
	m.cursorGroup = 0
	m.cursorItem = -1
}

// hasActiveFilters returns true if any filter is active.
func (m *Model) hasActiveFilters() bool {
	typeValue := m.typeWheel.SelectedValue()
	statusValue := m.statusWheel.SelectedValue()
	return (typeValue != "" && typeValue != "All") ||
		(statusValue != "" && statusValue != "All") ||
		m.conceptFilterPath != "" ||
		(m.searchInput.Value() != "" && m.focus != focusSearch)
}

// clearAllFilters resets all filter values to their defaults.
func (m *Model) clearAllFilters() {
	m.typeWheel.SetSelected(0)   // "All"
	m.statusWheel.SetSelected(0) // "All"
	m.conceptFilterPath = ""
	m.searchInput.SetValue("")
	m.applyFilters()
	m.statusMessage = "Filters cleared"
}
