// Package tui provides terminal UI components for intent management.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// Update implements tea.Model.
func (m IntentViewerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Handle confirmation dialog
	if m.showConfirm {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			m.confirmDialog.HandleKey(msg.String())
			if m.confirmDialog.IsDone() {
				m.showConfirm = false
				if m.confirmDialog.Confirmed() {
					switch m.pendingAction {
					case "delete":
						return m, m.deleteIntent()
					case "archive":
						return m, m.archiveIntent()
					}
				}
				m.pendingAction = ""
			}
		}
		return m, nil
	}

	// Handle move overlay
	if m.moveOverlay {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.moveOverlay = false
				return m, nil
			case "j", "down":
				if m.moveStatusIdx < len(moveStatusOptions)-1 {
					m.moveStatusIdx++
				}
			case "k", "up":
				if m.moveStatusIdx > 0 {
					m.moveStatusIdx--
				}
			case "enter":
				newStatus := moveStatusOptions[m.moveStatusIdx].status
				if m.intent.Status != newStatus {
					m.moveOverlay = false
					return m, m.moveIntent(newStatus)
				}
				m.moveOverlay = false
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		// Exit keys
		case "q", "esc", "backspace":
			return m, m.closeViewer()

		// Vim scrolling
		case "j", "down":
			m.viewport.ScrollDown(1)
		case "k", "up":
			m.viewport.ScrollUp(1)
		case "ctrl+d":
			m.viewport.HalfPageDown()
		case "ctrl+u":
			m.viewport.HalfPageUp()
		case "ctrl+f", "pgdown":
			m.viewport.PageDown()
		case "ctrl+b", "pgup":
			m.viewport.PageUp()
		case "home":
			m.viewport.GotoTop()
		case "G", "end":
			m.viewport.GotoBottom()
		case "g":
			// Gather-similar: find similar intents and show selection overlay
			if m.gatherSvc != nil {
				return m, m.findSimilarIntents()
			}
		case "H":
			// Jump to screen top (already visible)
			m.viewport.GotoTop()
		case "M":
			// Jump to screen middle
			m.viewport.SetYOffset(m.viewport.YOffset + m.viewport.Height/2)
		case "L":
			// Jump to screen bottom
			lines := strings.Count(m.content, "\n")
			targetLine := m.viewport.YOffset + m.viewport.Height - 1
			targetLine = min(targetLine, lines)
			m.viewport.SetYOffset(targetLine - m.viewport.Height + 1)

		// Sibling navigation (cycle through intents in same status group)
		case "left", "h":
			if len(m.siblings) > 1 {
				m.navigatePrev()
			}
			return m, nil
		case "right", "l":
			if len(m.siblings) > 1 {
				m.navigateNext()
			}
			return m, nil

		// Actions
		case "e":
			return m, m.openInEditor()
		case "m":
			m.moveOverlay = true
			m.moveStatusIdx = 0
			return m, nil
		case "p":
			// Promote to next status
			nextStatus := getNextStatus(m.intent.Status)
			if nextStatus != m.intent.Status {
				return m, m.moveIntent(nextStatus)
			}
		case "a":
			// Archive - requires confirmation
			if m.intent.Status != intent.StatusKilled {
				m.showConfirm = true
				m.pendingAction = "archive"
				m.confirmDialog = NewConfirmationDialog(
					"Archive Intent",
					fmt.Sprintf("Archive '%s'?\n\nIt will be moved to killed status.", m.intent.Title),
				)
			}
			return m, nil
		case "d":
			// Delete - requires confirmation
			m.showConfirm = true
			m.pendingAction = "delete"
			m.confirmDialog = NewConfirmationDialog(
				"Delete Intent",
				fmt.Sprintf("Delete '%s'?\n\nThis cannot be undone.", m.intent.Title),
			)
			return m, nil
		case "o":
			return m, m.openWithSystem()
		case "O":
			return m, m.revealInFileManager()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 4
		m.viewport.Height = msg.Height - 8
		m.renderContent()

	case ViewerEditorFinishedMsg:
		m.refreshOnReturn = true
		m.loadContent() // Reload content after edit
		return m, nil

	case ViewerMoveFinishedMsg:
		if msg.Err == nil {
			m.intent.Status = msg.NewStatus
			m.refreshOnReturn = true
		}
		return m, nil

	case ViewerArchiveFinishedMsg:
		if msg.Err == nil {
			m.refreshOnReturn = true
			return m, m.closeViewer()
		}
		return m, nil

	case ViewerDeleteFinishedMsg:
		if msg.Err == nil {
			m.refreshOnReturn = true
			return m, m.closeViewer()
		}
		return m, nil

	case ViewerSimilarFoundMsg:
		if msg.Err != nil {
			// Could show error message, for now just ignore
			return m, nil
		}
		// Show overlay even if empty so user sees "No similar intents found"
		m.similarIntents = msg.Similar
		m.gatherOverlay = true
		m.gatherCursorIdx = 0
		m.selectedSimilar = make(map[string]bool)
		return m, nil

	case ViewerGatherFinishedMsg:
		if msg.Err != nil {
			// Could show error message, for now close overlay
			m.showGatherTitle = false
			m.gatherOverlay = false
			m.selectedSimilar = make(map[string]bool)
			return m, nil
		}
		// Gather succeeded - close viewer and refresh
		m.refreshOnReturn = true
		return m, m.closeViewer()
	}

	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
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

// openInEditor opens the intent in $EDITOR.
func (m IntentViewerModel) openInEditor() tea.Cmd {
	if _, err := os.Stat(m.intent.Path); os.IsNotExist(err) {
		return func() tea.Msg {
			return ViewerEditorFinishedMsg{
				Err:  fmt.Errorf("file no longer exists: %s", filepath.Base(m.intent.Path)),
				Path: m.intent.Path,
			}
		}
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	c := exec.Command(editor, m.intent.Path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return ViewerEditorFinishedMsg{Err: err, Path: m.intent.Path}
	})
}

// moveIntent moves the intent to a new status.
func (m IntentViewerModel) moveIntent(newStatus intent.Status) tea.Cmd {
	return func() tea.Msg {
		_, err := m.service.Move(m.ctx, m.intent.ID, newStatus)
		return ViewerMoveFinishedMsg{
			Err:       err,
			NewStatus: newStatus,
		}
	}
}

// archiveIntent archives the intent.
func (m IntentViewerModel) archiveIntent() tea.Cmd {
	return func() tea.Msg {
		_, err := m.service.Archive(m.ctx, m.intent.ID)
		return ViewerArchiveFinishedMsg{Err: err}
	}
}

// deleteIntent deletes the intent.
func (m IntentViewerModel) deleteIntent() tea.Cmd {
	return func() tea.Msg {
		err := m.service.Delete(m.ctx, m.intent.ID)
		return ViewerDeleteFinishedMsg{Err: err}
	}
}

// openWithSystem opens the intent with the system handler.
func (m IntentViewerModel) openWithSystem() tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", m.intent.Path)
		case "linux":
			cmd = exec.Command("xdg-open", m.intent.Path)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", "", m.intent.Path)
		default:
			return nil
		}
		_ = cmd.Start() // Intentionally ignoring error for background process
		return nil
	}
}

// revealInFileManager reveals the file in the file manager.
func (m IntentViewerModel) revealInFileManager() tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", "-R", m.intent.Path)
		case "linux":
			cmd = exec.Command("xdg-open", filepath.Dir(m.intent.Path))
		case "windows":
			cmd = exec.Command("explorer", "/select,", m.intent.Path)
		default:
			return nil
		}
		_ = cmd.Start() // Intentionally ignoring error for background process
		return nil
	}
}

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
// It builds the index if needed before searching.
func (m IntentViewerModel) findSimilarIntents() tea.Cmd {
	return func() tea.Msg {
		// Build index if empty (lazy initialization)
		if m.gatherSvc.IndexSize() == 0 {
			if err := m.gatherSvc.BuildIndex(m.ctx); err != nil {
				return ViewerSimilarFoundMsg{Err: err}
			}
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

// View implements tea.Model.
func (m IntentViewerModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Handle overlays
	if m.showConfirm {
		return m.viewWithConfirmOverlay()
	}
	if m.moveOverlay {
		return m.viewWithMoveOverlay()
	}
	if m.showGatherTitle {
		return m.viewWithGatherTitleOverlay()
	}
	if m.gatherOverlay {
		return m.viewWithGatherSimilarOverlay()
	}

	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Separator
	separator := lipgloss.NewStyle().
		Foreground(pal.Border).
		Render(strings.Repeat("─", m.width-2))
	b.WriteString(separator)
	b.WriteString("\n")

	// Content viewport
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// Footer
	b.WriteString(separator)
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return viewerBoxStyle.
		Width(m.width).
		Height(m.height).
		Render(b.String())
}

// renderHeader renders the header with intent metadata.
func (m IntentViewerModel) renderHeader() string {
	// DEBUG: Show path and content info
	debugInfo := fmt.Sprintf("[DEBUG: Path=%s ContentLen=%d ViewportH=%d]",
		m.intent.Path, len(m.content), m.viewport.Height)

	// Title line
	title := viewerTitleStyle.Render(m.intent.Title + "\n" + debugInfo)

	// Metadata line
	typeBadge := viewerBadgeStyle.Render(fmt.Sprintf("[%s]", m.intent.Type))
	statusBadge := m.renderStatusBadge(m.intent.Status)
	concept := viewerMetaStyle.Render(m.intent.ConceptName())
	date := viewerMetaStyle.Render(formatRelativeTime(m.intent.CreatedAt))

	// Adjust based on width
	if m.width < 60 {
		// Minimal header for narrow terminals
		return title
	}

	meta := fmt.Sprintf("Type: %s  Status: %s  Concept: %s  Created: %s",
		typeBadge, statusBadge, concept, date)

	return lipgloss.JoinVertical(lipgloss.Left, title, meta)
}

// renderStatusBadge renders a colored status badge.
func (m IntentViewerModel) renderStatusBadge(s intent.Status) string {
	return renderStatusBadge(s)
}

// renderStatusBadge renders a colored status badge (shared helper).
func renderStatusBadge(s intent.Status) string {
	var color lipgloss.TerminalColor
	switch s {
	case intent.StatusInbox:
		color = pal.Warning // Orange
	case intent.StatusActive:
		color = pal.Success // Green
	case intent.StatusReady:
		color = pal.AccentAlt // Blue
	case intent.StatusDone:
		color = pal.TextMuted // Gray
	case intent.StatusKilled:
		color = pal.Error // Red
	default:
		color = pal.TextMuted
	}
	return lipgloss.NewStyle().Foreground(color).Render(s.String())
}

// renderFooter renders the footer with actions and scroll position.
func (m IntentViewerModel) renderFooter() string {
	// Actions - include gather if gather service is available
	actions := "[e]dit  [m]ove  [p]romote  [a]rchive  [d]elete  [o]pen  [O] reveal"
	if m.gatherSvc != nil {
		actions = "[e]dit  [g]ather  [m]ove  [p]romote  [a]rchive  [d]elete  [o]pen  [O] reveal"
	}

	// Scroll percentage
	scrollPct := int(m.viewport.ScrollPercent() * 100)
	scrollInfo := fmt.Sprintf("%d%%", scrollPct)

	// Position indicator (only if multiple siblings for navigation)
	var posInfo string
	if len(m.siblings) > 1 {
		posInfo = fmt.Sprintf("%d/%d │ ", m.currentIndex+1, len(m.siblings))
	}

	// Navigation hint - show arrow keys when navigation is available
	var navHint string
	if len(m.siblings) > 1 {
		navHint = "←/→: prev/next • q: back"
	} else {
		navHint = "q: back to list"
	}

	// Calculate spacing
	actionsWidth := lipgloss.Width(actions)
	scrollWidth := lipgloss.Width(scrollInfo)
	posWidth := lipgloss.Width(posInfo)
	navWidth := lipgloss.Width(navHint)
	padding := m.width - actionsWidth - scrollWidth - posWidth - navWidth - 10

	if padding < 0 {
		// Narrow terminal - minimal footer with position
		return viewerFooterStyle.Render(fmt.Sprintf("%s%s │ %s", posInfo, scrollInfo, navHint))
	}

	spacer := strings.Repeat(" ", padding)
	return viewerFooterStyle.Render(fmt.Sprintf("%s%s%s%s │ %s", actions, spacer, posInfo, scrollInfo, navHint))
}

// viewWithConfirmOverlay renders the view with confirmation dialog overlay.
func (m IntentViewerModel) viewWithConfirmOverlay() string {
	var b strings.Builder
	b.WriteString(viewerTitleStyle.Render("Intent Viewer"))
	b.WriteString("\n\n")
	b.WriteString(m.confirmDialog.View())
	return b.String()
}

// viewWithMoveOverlay renders the view with move status overlay.
func (m IntentViewerModel) viewWithMoveOverlay() string {
	var b strings.Builder

	b.WriteString(viewerTitleStyle.Render("Move Intent"))
	b.WriteString("\n\n")
	b.WriteString("Moving: " + m.intent.Title + "\n")
	b.WriteString("Current status: " + m.intent.Status.String() + "\n\n")
	b.WriteString("Select new status:\n")

	for i, opt := range moveStatusOptions {
		cursor := "  "
		if i == m.moveStatusIdx {
			cursor = "> "
		}
		marker := ""
		if m.intent.Status == opt.status {
			marker = " (current)"
		}
		b.WriteString(cursor + opt.name + marker + "\n")
	}

	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("j/k: navigate • Enter: move • Esc: cancel"))

	return b.String()
}

// viewWithGatherSimilarOverlay renders the view with gather-similar selection overlay.
func (m IntentViewerModel) viewWithGatherSimilarOverlay() string {
	var b strings.Builder

	b.WriteString(viewerTitleStyle.Render("Gather Similar Intents"))
	b.WriteString("\n\n")
	b.WriteString("Current: " + m.intent.Title + "\n\n")
	b.WriteString("Select similar intents to gather:\n")

	if len(m.similarIntents) == 0 {
		b.WriteString(HelpStyle.Render("  No similar intents found.\n"))
	} else {
		for i, sim := range m.similarIntents {
			cursor := "  "
			if i == m.gatherCursorIdx {
				cursor = "> "
			}
			checkbox := "[ ]"
			if m.selectedSimilar[sim.Intent.ID] {
				checkbox = "[x]"
			}
			// Show title and similarity score
			title := sim.Intent.Title
			if len(title) > 40 {
				title = title[:37] + "..."
			}
			score := fmt.Sprintf("%.0f%%", sim.Score*100)
			b.WriteString(fmt.Sprintf("%s%s %s (%s)\n", cursor, checkbox, title, score))
		}
	}

	selectedCount := len(m.selectedSimilar)
	b.WriteString("\n")
	if selectedCount > 0 {
		b.WriteString(fmt.Sprintf("Selected: %d intent(s)\n", selectedCount))
	}
	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("j/k: navigate • Space: toggle • Enter: proceed • Esc: cancel"))

	return b.String()
}

// viewWithGatherTitleOverlay renders the gather dialog for title input.
func (m IntentViewerModel) viewWithGatherTitleOverlay() string {
	var b strings.Builder

	b.WriteString(viewerTitleStyle.Render("Gather Intents"))
	b.WriteString("\n\n")
	b.WriteString(m.gatherDialog.View())

	return b.String()
}

// Styles for the intent viewer.
var (
	viewerBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(pal.BorderFocus).
			Padding(0, 1)

	viewerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Accent)

	viewerBadgeStyle = lipgloss.NewStyle().
				Foreground(pal.TextSecondary)

	viewerMetaStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)

	viewerFooterStyle = lipgloss.NewStyle().
				Foreground(pal.TextMuted)
)

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
