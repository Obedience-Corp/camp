// Package tui provides terminal UI components for intent management.
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/gather"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// IntentViewerModel is a full-screen viewer for reading intent documents.
// It provides viewport-based scrolling with vim navigation.
type IntentViewerModel struct {
	// Data
	intent     *intent.Intent
	content    string // Rendered markdown content
	rawContent string // Original content for re-rendering
	service    *intent.IntentService
	ctx        context.Context

	// Sibling navigation - allows cycling through intents in same status group
	siblings     []*intent.Intent // All intents in the same status group
	currentIndex int              // Index of current intent in siblings

	// Viewport
	viewport viewport.Model

	// Display
	width  int
	height int
	ready  bool

	// Overlays
	confirmDialog ConfirmationDialog
	showConfirm   bool
	moveOverlay   bool
	moveStatusIdx int

	// Gather-similar overlay
	gatherSvc       *gather.Service        // Gather service for finding similar intents
	gatherOverlay   bool                   // Whether gather-similar overlay is shown
	similarIntents  []gather.SimilarResult // Similar intents found
	selectedSimilar map[string]bool        // Selected similar intent IDs
	gatherCursorIdx int                    // Cursor position in similar list
	gatherDialog    GatherDialog           // Title input dialog
	showGatherTitle bool                   // Whether title input is shown

	// Navigation return
	refreshOnReturn bool // True if intent was modified

	// Pending action state (for confirmations)
	pendingAction string

	// Multi-key sequences (vim-style gg)
	pendingKey string

	// Search
	searchMode     bool            // Whether search input is active
	searchInput    textinput.Model // The search text input
	searchQuery    string          // Active search query (persists after exiting input)
	searchMatches  []int           // Line numbers with matches
	searchMatchIdx int             // Current match index (-1 = none)
}

// ViewerClosedMsg is sent when the viewer is closed.
type ViewerClosedMsg struct {
	IntentID   string
	Refresh    bool // True if intent was modified
	FinalIndex int  // Index of intent when viewer closed (for cursor sync)
}

// ViewerEditorFinishedMsg is sent when editor closes from viewer.
type ViewerEditorFinishedMsg struct {
	Err  error
	Path string
}

// ViewerMoveFinishedMsg is sent when move completes from viewer.
type ViewerMoveFinishedMsg struct {
	Err       error
	NewStatus intent.Status
}

// ViewerArchiveFinishedMsg is sent when archive completes from viewer.
type ViewerArchiveFinishedMsg struct {
	Err error
}

// ViewerDeleteFinishedMsg is sent when delete completes from viewer.
type ViewerDeleteFinishedMsg struct {
	Err error
}

// NewIntentViewerModel creates a new intent viewer for the given intent.
// siblings contains all intents in the same status group for left/right navigation.
// currentIndex is the position of the current intent within siblings.
func NewIntentViewerModel(ctx context.Context, i *intent.Intent, siblings []*intent.Intent, currentIndex int, svc *intent.IntentService, width, height int) IntentViewerModel {
	return NewIntentViewerModelWithGather(ctx, i, siblings, currentIndex, svc, nil, width, height)
}

// NewIntentViewerModelWithGather creates a viewer with gather service for gather-similar feature.
func NewIntentViewerModelWithGather(ctx context.Context, i *intent.Intent, siblings []*intent.Intent, currentIndex int, svc *intent.IntentService, gatherSvc *gather.Service, width, height int) IntentViewerModel {
	vp := viewport.New(width-4, height-8) // Account for header, footer, borders
	vp.Style = lipgloss.NewStyle().Padding(0, 1)

	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.CharLimit = 100

	m := IntentViewerModel{
		intent:          i,
		siblings:        siblings,
		currentIndex:    currentIndex,
		service:         svc,
		gatherSvc:       gatherSvc,
		ctx:             ctx,
		viewport:        vp,
		width:           width,
		height:          height,
		ready:           true,
		selectedSimilar: make(map[string]bool),
		searchInput:     ti,
		searchMatchIdx:  -1,
	}

	// Load and render content
	m.loadContent()

	return m
}

// loadContent reads the intent file and renders markdown.
func (m *IntentViewerModel) loadContent() {
	if m.intent == nil {
		m.content = "No content available."
		m.viewport.SetContent(m.content)
		return
	}

	if m.intent.Path == "" {
		m.content = "No file path for this intent."
		m.viewport.SetContent(m.content)
		return
	}

	data, err := os.ReadFile(m.intent.Path)
	if err != nil {
		m.content = fmt.Sprintf("Error reading file: %s", err.Error())
		m.viewport.SetContent(m.content)
		return
	}

	if len(data) == 0 {
		m.content = "File is empty."
		m.viewport.SetContent(m.content)
		return
	}

	m.rawContent = string(data)

	stripped := stripFrontmatter(m.rawContent)
	if strings.TrimSpace(stripped) == "" {
		m.content = "No content after frontmatter."
		m.viewport.SetContent(m.content)
		return
	}

	m.renderContent()
}

// renderContent renders the markdown content.
func (m *IntentViewerModel) renderContent() {
	content := stripFrontmatter(m.rawContent)
	m.content = renderMarkdown(content, m.width-6)
	// Apply search highlights if active
	if m.searchQuery != "" {
		m.viewport.SetContent(m.applySearchHighlight(m.content))
	} else {
		m.viewport.SetContent(m.content)
	}
}

// Init implements tea.Model.
func (m IntentViewerModel) Init() tea.Cmd {
	return nil
}

// closeViewer returns a command to close the viewer.
func (m IntentViewerModel) closeViewer() tea.Cmd {
	return func() tea.Msg {
		return ViewerClosedMsg{
			IntentID:   m.intent.ID,
			Refresh:    m.refreshOnReturn,
			FinalIndex: m.currentIndex,
		}
	}
}

// navigatePrev moves to the previous intent in the sibling list (wraps around).
func (m *IntentViewerModel) navigatePrev() {
	if len(m.siblings) == 0 {
		return // Safety check
	}
	if m.currentIndex > 0 {
		m.currentIndex--
	} else {
		m.currentIndex = len(m.siblings) - 1 // wrap to end
	}
	if m.currentIndex >= 0 && m.currentIndex < len(m.siblings) {
		m.intent = m.siblings[m.currentIndex]
		m.loadContent()
		m.viewport.GotoTop()
	}
}

// navigateNext moves to the next intent in the sibling list (wraps around).
func (m *IntentViewerModel) navigateNext() {
	if len(m.siblings) == 0 {
		return // Safety check
	}
	if m.currentIndex < len(m.siblings)-1 {
		m.currentIndex++
	} else {
		m.currentIndex = 0 // wrap to start
	}
	if m.currentIndex >= 0 && m.currentIndex < len(m.siblings) {
		m.intent = m.siblings[m.currentIndex]
		m.loadContent()
		m.viewport.GotoTop()
	}
}

// findSearchMatches scans content lines for case-insensitive substring matches.
func (m *IntentViewerModel) findSearchMatches() {
	m.searchMatches = nil
	m.searchMatchIdx = -1

	if m.searchQuery == "" {
		return
	}

	query := strings.ToLower(m.searchQuery)
	lines := strings.Split(m.content, "\n")
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), query) {
			m.searchMatches = append(m.searchMatches, i)
		}
	}

	if len(m.searchMatches) > 0 {
		m.searchMatchIdx = 0
	}
}

// scrollToMatch sets viewport offset to show the current match line.
func (m *IntentViewerModel) scrollToMatch() {
	if m.searchMatchIdx < 0 || m.searchMatchIdx >= len(m.searchMatches) {
		return
	}
	lineNum := m.searchMatches[m.searchMatchIdx]
	// Center the match in the viewport
	offset := lineNum - m.viewport.Height/2
	if offset < 0 {
		offset = 0
	}
	m.viewport.SetYOffset(offset)
}

// nextMatch jumps to the next search match.
func (m *IntentViewerModel) nextMatch() {
	if len(m.searchMatches) == 0 {
		return
	}
	m.searchMatchIdx = (m.searchMatchIdx + 1) % len(m.searchMatches)
	m.scrollToMatch()
}

// prevMatch jumps to the previous search match.
func (m *IntentViewerModel) prevMatch() {
	if len(m.searchMatches) == 0 {
		return
	}
	m.searchMatchIdx--
	if m.searchMatchIdx < 0 {
		m.searchMatchIdx = len(m.searchMatches) - 1
	}
	m.scrollToMatch()
}

// applySearchHighlight returns content with search matches highlighted.
func (m IntentViewerModel) applySearchHighlight(content string) string {
	if m.searchQuery == "" {
		return content
	}

	highlightStyle := lipgloss.NewStyle().
		Background(pal.Warning).
		Foreground(lipgloss.Color("#000000"))

	query := strings.ToLower(m.searchQuery)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		if idx := strings.Index(lower, query); idx >= 0 {
			// Highlight the first occurrence on each matching line
			matchText := line[idx : idx+len(m.searchQuery)]
			lines[i] = line[:idx] + highlightStyle.Render(matchText) + line[idx+len(m.searchQuery):]
		}
	}
	return strings.Join(lines, "\n")
}
