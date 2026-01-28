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
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/intent"
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

	// Navigation return
	refreshOnReturn bool // True if intent was modified

	// Pending action state (for confirmations)
	pendingAction string
}

// viewerClosedMsg is sent when the viewer is closed.
type viewerClosedMsg struct {
	intentID   string
	refresh    bool // True if intent was modified
	finalIndex int  // Index of intent when viewer closed (for cursor sync)
}

// viewerEditorFinishedMsg is sent when editor closes from viewer.
type viewerEditorFinishedMsg struct {
	err  error
	path string
}

// viewerMoveFinishedMsg is sent when move completes from viewer.
type viewerMoveFinishedMsg struct {
	err       error
	newStatus intent.Status
}

// viewerArchiveFinishedMsg is sent when archive completes from viewer.
type viewerArchiveFinishedMsg struct {
	err error
}

// viewerDeleteFinishedMsg is sent when delete completes from viewer.
type viewerDeleteFinishedMsg struct {
	err error
}

// NewIntentViewerModel creates a new intent viewer for the given intent.
// siblings contains all intents in the same status group for left/right navigation.
// currentIndex is the position of the current intent within siblings.
func NewIntentViewerModel(ctx context.Context, i *intent.Intent, siblings []*intent.Intent, currentIndex int, svc *intent.IntentService, width, height int) IntentViewerModel {
	vp := viewport.New(width-4, height-8) // Account for header, footer, borders
	vp.Style = lipgloss.NewStyle().Padding(0, 1)

	m := IntentViewerModel{
		intent:       i,
		siblings:     siblings,
		currentIndex: currentIndex,
		service:      svc,
		ctx:          ctx,
		viewport:     vp,
		width:        width,
		height:       height,
		ready:        true,
	}

	// Load and render content
	m.loadContent()

	return m
}

// loadContent reads the intent file and renders markdown.
func (m *IntentViewerModel) loadContent() {
	if m.intent == nil || m.intent.Path == "" {
		m.content = "No content available"
		m.viewport.SetContent(m.content)
		return
	}

	data, err := os.ReadFile(m.intent.Path)
	if err != nil {
		m.content = "Error loading content: " + err.Error()
		m.viewport.SetContent(m.content)
		return
	}

	m.rawContent = string(data)
	m.renderContent()
}

// renderContent renders the markdown content.
func (m *IntentViewerModel) renderContent() {
	content := stripFrontmatter(m.rawContent)

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(m.width-6),
	)
	if err != nil {
		m.content = content
		m.viewport.SetContent(m.content)
		return
	}

	rendered, err := renderer.Render(content)
	if err != nil {
		m.content = content
		m.viewport.SetContent(m.content)
		return
	}

	m.content = strings.TrimSpace(rendered)
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
		case "g", "home":
			m.viewport.GotoTop()
		case "G", "end":
			m.viewport.GotoBottom()
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

	case viewerEditorFinishedMsg:
		m.refreshOnReturn = true
		m.loadContent() // Reload content after edit
		return m, nil

	case viewerMoveFinishedMsg:
		if msg.err == nil {
			m.intent.Status = msg.newStatus
			m.refreshOnReturn = true
		}
		return m, nil

	case viewerArchiveFinishedMsg:
		if msg.err == nil {
			// Return to explorer after archive
			return m, m.closeViewer()
		}
		return m, nil

	case viewerDeleteFinishedMsg:
		if msg.err == nil {
			// Return to explorer after delete
			return m, m.closeViewer()
		}
		return m, nil
	}

	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// closeViewer returns a command to close the viewer.
func (m IntentViewerModel) closeViewer() tea.Cmd {
	return func() tea.Msg {
		return viewerClosedMsg{
			intentID:   m.intent.ID,
			refresh:    m.refreshOnReturn,
			finalIndex: m.currentIndex,
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
			return viewerEditorFinishedMsg{
				err:  fmt.Errorf("file no longer exists: %s", filepath.Base(m.intent.Path)),
				path: m.intent.Path,
			}
		}
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	c := exec.Command(editor, m.intent.Path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return viewerEditorFinishedMsg{err: err, path: m.intent.Path}
	})
}

// moveIntent moves the intent to a new status.
func (m IntentViewerModel) moveIntent(newStatus intent.Status) tea.Cmd {
	return func() tea.Msg {
		_, err := m.service.Move(m.ctx, m.intent.ID, newStatus)
		return viewerMoveFinishedMsg{
			err:       err,
			newStatus: newStatus,
		}
	}
}

// archiveIntent archives the intent.
func (m IntentViewerModel) archiveIntent() tea.Cmd {
	return func() tea.Msg {
		_, err := m.service.Archive(m.ctx, m.intent.ID)
		return viewerArchiveFinishedMsg{err: err}
	}
}

// deleteIntent deletes the intent.
func (m IntentViewerModel) deleteIntent() tea.Cmd {
	return func() tea.Msg {
		err := m.service.Delete(m.ctx, m.intent.ID)
		return viewerDeleteFinishedMsg{err: err}
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
	// Title line
	title := viewerTitleStyle.Render(m.intent.Title)

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
	// Actions
	actions := "[e]dit  [m]ove  [p]romote  [a]rchive  [d]elete  [o]pen  [O] reveal"

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
	b.WriteString(helpStyle.Render("j/k: navigate • Enter: move • Esc: cancel"))

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
