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

	// Reserve space for header (title, filters) and footer (help text)
	headerHeight := 4 // Title + filters + spacing
	footerHeight := 3 // Help text + status message
	contentHeight := m.height - headerHeight - footerHeight
	contentHeight = max(contentHeight, 5)

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
			// More generous 50/50 split for wide terminals
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
