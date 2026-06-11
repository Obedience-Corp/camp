package explorer

import (
	"strings"

	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/sahilm/fuzzy"
)

// typeFilterItems are the available type filter options.
var typeFilterItems = []string{"All", "Idea", "Feature", "Bug", "Research", "Chore"}

// statusFilterItems are the available status filter options.
var statusFilterItems = []string{"All", "Notes", "Inbox", "Ready", "Active", "Done", "Killed"}

type explorerItemSource []*intent.Intent

func (s explorerItemSource) String(i int) string {
	item := s[i]
	return strings.Join([]string{item.Title, item.ID, item.Content}, " ")
}

func (s explorerItemSource) Len() int { return len(s) }

// applyFilters filters intents using search query and type filter.
func (m *Model) applyFilters() {
	m.statusMessage = ""

	// Notes mode: the intent-oriented filters (type/status/concept) and the
	// intent search index do not apply. Show the loaded notes as-is.
	if m.notesMode {
		m.filteredIntents = m.intents
		m.rebuildStatusGroups()
		m.cursorGroup = 0
		m.cursorItem = -1
		m.scrollOffset = 0
		return
	}

	query := m.searchInput.Value()

	// Start with all intents
	var filtered []*intent.Intent

	// Apply search if there's a query. Search the loaded explorer items directly
	// so notes remain searchable in the combined default view.
	if query == "" {
		filtered = m.intents
	} else {
		matches := fuzzy.FindFrom(query, explorerItemSource(m.intents))
		filtered = make([]*intent.Intent, len(matches))
		for i, match := range matches {
			filtered[i] = m.intents[match.Index]
		}
	}

	// Apply type filter from filter bar
	typeChip := m.filterBar.ChipByLabel("Type")
	if typeChip != nil {
		typeSelection := typeChip.SelectedValue()
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
	}

	// Apply status filter from filter bar
	statusChip := m.filterBar.ChipByLabel("Status")
	if statusChip != nil {
		statusSelection := statusChip.SelectedValue()
		if statusSelection != "All" && statusSelection != "" {
			statusFiltered := make([]*intent.Intent, 0)
			if targetStatus, ok := statusSelectionToStatus(statusSelection); ok {
				for _, i := range filtered {
					if i.Status == targetStatus {
						statusFiltered = append(statusFiltered, i)
					}
				}
				filtered = statusFiltered
			}
		}
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

	// Rebuild groups from filtered explorer items
	m.groups = groupExplorerItemsByStatus(m.filteredIntents, m.dungeonExpanded)

	// Reset cursor position and scroll
	m.cursorGroup = 0
	m.cursorItem = -1
	m.scrollOffset = 0
}

// hasActiveFilters returns true if any filter is active.
func (m *Model) hasActiveFilters() bool {
	// Check filter bar chips
	if m.filterBar.HasActiveFilters() {
		return true
	}
	// Check concept filter
	if m.conceptFilterPath != "" {
		return true
	}
	// Check search
	if m.searchInput.Value() != "" && m.focus != focusSearch {
		return true
	}
	return false
}

// clearAllFilters resets all filter values to their defaults.
func (m *Model) clearAllFilters() {
	m.filterBar.ClearAll()
	m.conceptFilterPath = ""
	m.searchInput.SetValue("")
	m.applyFilters()
	m.statusMessage = "Filters cleared"
}

func statusSelectionToStatus(selection string) (intent.Status, bool) {
	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "notes":
		return intent.StatusNote, true
	case "inbox":
		return intent.StatusInbox, true
	case "ready":
		return intent.StatusReady, true
	case "active":
		return intent.StatusActive, true
	case "done", "dungeon/done":
		return intent.StatusDone, true
	case "killed", "dungeon/killed":
		return intent.StatusKilled, true
	case "archived", "dungeon/archived":
		return intent.StatusArchived, true
	case "someday", "dungeon/someday":
		return intent.StatusSomeday, true
	default:
		return "", false
	}
}
