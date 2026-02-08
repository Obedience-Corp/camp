package explorer

import (
	"fmt"
	"os"

	"github.com/obediencecorp/camp/internal/intent"
)

// layoutMode determines the responsive layout based on terminal width.
type layoutMode int

const (
	layoutNarrow layoutMode = iota // <80 columns
	layoutNormal                   // 80-120 columns
	layoutWide                     // >120 columns
)

// Width breakpoints for responsive layout.
const (
	breakpointNarrow = 80
	breakpointWide   = 120
)

// getLayoutMode returns the current layout mode based on terminal width.
func (m *Model) getLayoutMode() layoutMode {
	switch {
	case m.width < breakpointNarrow:
		return layoutNarrow
	case m.width >= breakpointWide:
		return layoutWide
	default:
		return layoutNormal
	}
}

// recalculateLayout updates component sizes based on terminal dimensions.
func (m *Model) recalculateLayout() {
	if m.width == 0 || m.height == 0 {
		return
	}

	oldMode := m.layoutMode
	m.layoutMode = m.getLayoutMode()

	// Estimate content height for preview pane sizing.
	// The list height is computed dynamically per render in buildMainView(),
	// but the preview pane needs a size set here for its viewport.
	estimatedHeaderFooter := 8 // Conservative estimate for title + filters + status
	contentHeight := m.height - estimatedHeaderFooter
	contentHeight = max(contentHeight, 5)

	// Set list height estimate for scroll calculations in Update().
	// The exact value is computed per-render in buildMainView(), but we need
	// a working estimate here so ensureCursorVisible() can function.
	m.listHeight = max(m.height-estimatedHeaderFooter, 3)

	switch m.layoutMode {
	case layoutNarrow:
		// Force hide preview on narrow terminals
		m.previewForceHidden = true
		m.showConceptColumn = false
		m.fullConceptPaths = false

	case layoutNormal:
		m.previewForceHidden = false
		m.showConceptColumn = true
		m.fullConceptPaths = false

		if m.shouldShowPreview() {
			listWidth := max(m.width*60/100, 40)
			previewWidth := max(m.width-listWidth-2, 30)
			m.previewPane.SetSize(previewWidth, contentHeight)
		}

	case layoutWide:
		m.previewForceHidden = false
		m.showConceptColumn = true
		m.fullConceptPaths = true

		if m.shouldShowPreview() {
			listWidth := m.width * 50 / 100
			previewWidth := m.width - listWidth - 2
			m.previewPane.SetSize(previewWidth, contentHeight)
		}
	}

	// Notify user of layout mode change
	if oldMode != m.layoutMode && m.ready {
		switch m.layoutMode {
		case layoutNarrow:
			if m.showPreview {
				m.statusMessage = "Preview hidden (narrow terminal)"
			}
		case layoutNormal:
			if oldMode == layoutNarrow && m.showPreview {
				m.statusMessage = "Preview restored"
			}
		}
	}
}

// shouldShowPreview returns whether the preview pane should be displayed.
func (m *Model) shouldShowPreview() bool {
	if !m.showPreview {
		return false
	}
	if m.previewForceHidden {
		return false
	}
	return true
}

// loadPreviewContent loads content from an intent file into the preview pane.
func (m *Model) loadPreviewContent(i *intent.Intent) {
	if i == nil {
		m.previewPane.SetContent("DEBUG", "intent is nil")
		return
	}
	if i.Path == "" {
		m.previewPane.SetContent("DEBUG", fmt.Sprintf("intent.Path is empty\nID: %s\nTitle: %s", i.ID, i.Title))
		return
	}

	content, err := os.ReadFile(i.Path)
	if err != nil {
		m.previewPane.SetContent(i.Title, fmt.Sprintf("DEBUG: Error reading\nPath: %s\nError: %s", i.Path, err.Error()))
		return
	}

	if len(content) == 0 {
		m.previewPane.SetContent(i.Title, fmt.Sprintf("DEBUG: File is empty\nPath: %s", i.Path))
		return
	}

	m.previewPane.SetContent(i.Title, string(content))
}

// updatePreviewForSelection updates preview content when selection changes.
func (m *Model) updatePreviewForSelection() {
	if !m.showPreview {
		return
	}
	if selected := m.SelectedIntent(); selected != nil {
		m.loadPreviewContent(selected)
	}
}
