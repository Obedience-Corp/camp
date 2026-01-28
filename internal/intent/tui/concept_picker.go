package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/obediencecorp/camp/internal/concept"
)

// pickerStep represents the current step in the concept selection process.
type pickerStep int

const (
	stepSelectingType pickerStep = iota
	stepSelectingItem
	stepDone
)

// ConceptPickerModel provides a cascading selection UI for concepts.
// First, the user selects a concept type (e.g., Projects, Festivals).
// Then, they can drill into items within that concept.
type ConceptPickerModel struct {
	conceptSvc concept.Service
	concepts   []concept.Concept
	items      []concept.Item

	typeWheel ScrollWheel
	itemWheel ScrollWheel
	step      pickerStep

	// Selected state
	selectedConcept *concept.Concept
	currentSubpath  string
	pathHistory     []string // For backspace navigation

	// Result
	selectedPath string
	cancelled    bool

	// Context for service calls
	ctx context.Context
}

// NewConceptPickerModel creates a new concept picker with the given service.
func NewConceptPickerModel(ctx context.Context, svc concept.Service) ConceptPickerModel {
	concepts, err := svc.List(ctx)
	if err != nil {
		// Log error but continue with empty concepts
		concepts = nil
	}

	names := make([]string, len(concepts))
	for i, c := range concepts {
		names[i] = c.Name + " - " + c.Description
	}

	tw := NewScrollWheel(names)
	tw.Focus()

	return ConceptPickerModel{
		ctx:        ctx,
		conceptSvc: svc,
		concepts:   concepts,
		typeWheel:  tw,
		step:       stepSelectingType,
	}
}

// Init implements tea.Model.
func (m ConceptPickerModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m ConceptPickerModel) Update(msg tea.Msg) (ConceptPickerModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.step {
		case stepSelectingType:
			return m.updateTypeSelection(msg)
		case stepSelectingItem:
			return m.updateItemSelection(msg)
		}
	}

	return m, cmd
}

// updateTypeSelection handles key input during concept type selection.
func (m ConceptPickerModel) updateTypeSelection(msg tea.KeyMsg) (ConceptPickerModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.typeWheel, _ = m.typeWheel.Update(msg)
	case "down", "j":
		m.typeWheel, _ = m.typeWheel.Update(msg)
	case "enter":
		// Get selected concept and advance to item selection
		idx := m.typeWheel.Selected()
		if idx >= 0 && idx < len(m.concepts) {
			c := m.concepts[idx]
			m.selectedConcept = &c

			// If concept has items, advance to item selection
			if c.HasItems {
				m.step = stepSelectingItem
				m.loadItems("")
			} else {
				// No items, just use the concept path
				m.selectedPath = c.Path
				m.step = stepDone
			}
		}
	case "esc", "backspace", "left", "h":
		// Cancel picker from type selection
		m.cancelled = true
		m.step = stepDone
	}

	return m, nil
}

// updateItemSelection handles key input during item selection.
func (m ConceptPickerModel) updateItemSelection(msg tea.KeyMsg) (ConceptPickerModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.itemWheel, _ = m.itemWheel.Update(msg)
	case "down", "j":
		m.itemWheel, _ = m.itemWheel.Update(msg)
	case "enter":
		idx := m.itemWheel.Selected()
		if idx >= 0 && idx < len(m.items) {
			item := m.items[idx]
			// Check if depth is infinite (nil) or configured
			infiniteDepth := m.selectedConcept.MaxDepth == nil
			canDrill := item.IsDir && item.Children > 0 && !item.DrillDisabled

			if canDrill && !infiniteDepth {
				// Configured depth: auto-drill until max depth
				m.drillInto(item)
			} else {
				// Infinite depth OR can't drill: select this item
				m.selectedPath = item.Path
				m.step = stepDone
			}
		}
	case "right", "l":
		// Explicit drill (useful for infinite depth concepts)
		idx := m.itemWheel.Selected()
		if idx >= 0 && idx < len(m.items) {
			item := m.items[idx]
			if item.IsDir && item.Children > 0 && !item.DrillDisabled {
				m.drillInto(item)
			}
		}
	case "backspace", "left", "h":
		// Go back in path history
		m.navigateUp()
	case "esc":
		m.cancelled = true
		m.step = stepDone
	}

	return m, nil
}

// drillInto navigates into a subdirectory.
func (m *ConceptPickerModel) drillInto(item concept.Item) {
	m.pathHistory = append(m.pathHistory, m.currentSubpath)
	if m.currentSubpath == "" {
		m.currentSubpath = item.Name
	} else {
		m.currentSubpath = m.currentSubpath + "/" + item.Name
	}
	m.loadItems(m.currentSubpath)
}

// navigateUp goes back one level in the directory hierarchy.
func (m *ConceptPickerModel) navigateUp() {
	if len(m.pathHistory) > 0 {
		// Pop from history
		lastIdx := len(m.pathHistory) - 1
		previousPath := m.pathHistory[lastIdx]
		m.pathHistory = m.pathHistory[:lastIdx]

		// Reload items at previous path
		m.currentSubpath = previousPath
		m.loadItems(previousPath)
	} else if m.currentSubpath != "" {
		// At concept root with subpath, go to empty path
		m.currentSubpath = ""
		m.loadItems("")
	} else {
		// At concept root with empty history, go back to type selection
		m.step = stepSelectingType
		m.selectedConcept = nil
		m.items = nil
		m.currentSubpath = ""
		m.typeWheel.Focus()
	}
}

// loadItems loads items for the current concept and subpath.
func (m *ConceptPickerModel) loadItems(subpath string) {
	if m.selectedConcept == nil {
		return
	}

	items, err := m.conceptSvc.ListItems(m.ctx, m.selectedConcept.Name, subpath)
	if err != nil {
		m.items = nil
		return
	}

	m.items = items

	// Build names for the scroll wheel with directory indicators
	names := make([]string, len(items))
	for i, item := range items {
		if item.IsDir {
			if item.DrillDisabled {
				// Drilling disabled by depth limit - no arrow, no "(empty)"
				names[i] = "  " + item.Name
			} else if item.Children > 0 {
				names[i] = "▸ " + item.Name
			} else {
				names[i] = "  " + item.Name + " (empty)"
			}
		} else {
			names[i] = "  " + item.Name
		}
	}

	m.itemWheel = NewScrollWheel(names)
	m.itemWheel.Focus()
}

// View implements tea.Model.
func (m ConceptPickerModel) View() string {
	switch m.step {
	case stepSelectingType:
		return m.viewTypeSelection()
	case stepSelectingItem:
		return m.viewItemSelection()
	default:
		return ""
	}
}

// viewTypeSelection renders the concept type selection view.
func (m ConceptPickerModel) viewTypeSelection() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Select concept type:"))
	b.WriteString("\n\n")

	if len(m.concepts) == 0 {
		b.WriteString(helpStyle.Render("(no concepts configured)"))
	} else {
		b.WriteString(m.typeWheel.View())
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓: navigate • Enter: select • Esc: cancel"))

	return b.String()
}

// viewItemSelection renders the item selection view.
func (m ConceptPickerModel) viewItemSelection() string {
	var b strings.Builder

	// Show breadcrumb path with > separator for visual clarity
	breadcrumb := m.buildBreadcrumb()
	b.WriteString(titleStyle.Render("📁 " + breadcrumb))
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString(helpStyle.Render("(no items)"))
	} else {
		b.WriteString(m.itemWheel.View())
	}

	b.WriteString("\n")
	// Show different help based on depth mode
	if m.selectedConcept.MaxDepth == nil {
		// Infinite depth: Enter selects, Right/l drills
		b.WriteString(helpStyle.Render("↑/↓: navigate • Enter: select • →/l: drill • Backspace: back • Esc: cancel"))
	} else {
		// Configured depth: Enter auto-drills until max
		b.WriteString(helpStyle.Render("↑/↓: navigate • Enter: select • Backspace: back • Esc: cancel"))
	}

	return b.String()
}

// buildBreadcrumb creates a readable path string with > separators.
func (m ConceptPickerModel) buildBreadcrumb() string {
	parts := []string{m.selectedConcept.Name}

	if m.currentSubpath != "" {
		// Split the subpath by separator
		subparts := strings.Split(m.currentSubpath, "/")
		parts = append(parts, subparts...)
	}

	return strings.Join(parts, " > ")
}

// Done returns true if the picker is finished (selected or cancelled).
func (m ConceptPickerModel) Done() bool {
	return m.step == stepDone
}

// Cancelled returns true if the user cancelled the picker.
func (m ConceptPickerModel) Cancelled() bool {
	return m.cancelled
}

// SelectedPath returns the selected concept path, or empty if cancelled.
func (m ConceptPickerModel) SelectedPath() string {
	return m.selectedPath
}

// SelectedConcept returns the selected concept, or nil if cancelled.
func (m ConceptPickerModel) SelectedConcept() *concept.Concept {
	return m.selectedConcept
}
