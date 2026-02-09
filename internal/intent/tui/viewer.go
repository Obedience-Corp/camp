// Package tui provides terminal UI components for intent management.
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/intent/gather"
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
	}

	// Load and render content
	m.loadContent()

	return m
}

// loadContent reads the intent file and renders markdown.
func (m *IntentViewerModel) loadContent() {
	if m.intent == nil {
		m.content = "DEBUG: intent is nil"
		m.viewport.SetContent(m.content)
		return
	}

	if m.intent.Path == "" {
		m.content = fmt.Sprintf("DEBUG: intent.Path is empty\nIntent ID: %s\nIntent Title: %s", m.intent.ID, m.intent.Title)
		m.viewport.SetContent(m.content)
		return
	}

	data, err := os.ReadFile(m.intent.Path)
	if err != nil {
		m.content = fmt.Sprintf("DEBUG: Error reading file\nPath: %s\nError: %s", m.intent.Path, err.Error())
		m.viewport.SetContent(m.content)
		return
	}

	if len(data) == 0 {
		m.content = fmt.Sprintf("DEBUG: File is empty\nPath: %s", m.intent.Path)
		m.viewport.SetContent(m.content)
		return
	}

	m.rawContent = string(data)

	// Check what stripFrontmatter returns
	stripped := stripFrontmatter(m.rawContent)
	if strings.TrimSpace(stripped) == "" {
		m.content = fmt.Sprintf("DEBUG: Content empty after stripFrontmatter\nPath: %s\nRaw length: %d bytes\nFirst 500 chars:\n%s",
			m.intent.Path, len(data), truncate(m.rawContent, 500))
		m.viewport.SetContent(m.content)
		return
	}

	m.renderContent()
}

// renderContent renders the markdown content.
func (m *IntentViewerModel) renderContent() {
	content := stripFrontmatter(m.rawContent)
	m.content = renderMarkdown(content, m.width-6)
	m.viewport.SetContent(m.content)
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
