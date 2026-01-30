// Package tui provides terminal UI components for intent management.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PreviewPane displays the content of a selected intent in a scrollable view.
type PreviewPane struct {
	viewport   viewport.Model
	content    string // Rendered content
	rawContent string // Original content for re-rendering
	title      string
	width      int
	height     int
}

// NewPreviewPane creates a new preview pane with the given dimensions.
func NewPreviewPane(width, height int) PreviewPane {
	// Account for border in viewport dimensions
	vp := viewport.New(width-4, height-4)
	vp.Style = lipgloss.NewStyle().Padding(0, 1)

	return PreviewPane{
		viewport: vp,
		width:    width,
		height:   height,
	}
}

// SetContent sets the title and content to display.
// Content is rendered as markdown with frontmatter stripped.
func (p *PreviewPane) SetContent(title, rawContent string) {
	p.title = title
	p.rawContent = rawContent

	// Debug: check if raw content is empty
	if rawContent == "" {
		p.content = "DEBUG: rawContent is empty"
		p.viewport.SetContent(p.content)
		p.viewport.GotoTop()
		return
	}

	// Strip YAML frontmatter
	content := stripFrontmatter(rawContent)

	// Debug: check if content is empty after stripping
	if strings.TrimSpace(content) == "" {
		debugMsg := fmt.Sprintf("DEBUG: Empty after stripFrontmatter\nRaw length: %d\nFirst 300 chars:\n%s",
			len(rawContent), truncatePreview(rawContent, 300))
		p.content = debugMsg
		p.viewport.SetContent(debugMsg)
		p.viewport.GotoTop()
		return
	}

	// Render markdown
	rendered := p.renderPreviewMarkdown(content)

	p.content = rendered
	p.viewport.SetContent(rendered)
	p.viewport.GotoTop()
}

// truncatePreview truncates a string for debug display.
func truncatePreview(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// SetSize updates the dimensions of the preview pane.
func (p *PreviewPane) SetSize(width, height int) {
	p.width = width
	p.height = height
	p.viewport.Width = width - 4
	p.viewport.Height = height - 6 // Account for border and title

	// Re-render content with new width if we have content
	if p.rawContent != "" {
		content := stripFrontmatter(p.rawContent)
		rendered := p.renderPreviewMarkdown(content)
		p.content = rendered
		p.viewport.SetContent(rendered)
	}
}

// renderPreviewMarkdown renders content using the shared glamour renderer.
func (p *PreviewPane) renderPreviewMarkdown(content string) string {
	return renderMarkdown(content, p.width-6)
}

// stripFrontmatter removes YAML frontmatter from content.
func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}

	// Find closing ---
	endIdx := strings.Index(content[3:], "---")
	if endIdx == -1 {
		return content
	}

	// Return content after frontmatter (skip opening ---, content, closing ---)
	return strings.TrimSpace(content[endIdx+6:])
}

// Update handles scrolling input.
func (p PreviewPane) Update(msg tea.Msg) (PreviewPane, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			p.viewport.ScrollDown(1)
		case "k", "up":
			p.viewport.ScrollUp(1)
		case "ctrl+d":
			p.viewport.HalfPageDown()
		case "ctrl+u":
			p.viewport.HalfPageUp()
		case "g":
			p.viewport.GotoTop()
		case "G":
			p.viewport.GotoBottom()
		}
	}

	p.viewport, cmd = p.viewport.Update(msg)
	return p, cmd
}

// Styles for the preview pane.
var (
	previewBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(pal.Border)

	previewTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Accent)

	previewEmptyStyle = lipgloss.NewStyle().
				Foreground(pal.TextMuted).
				Italic(true)
)

// View renders the preview pane.
func (p PreviewPane) View() string {
	if p.content == "" {
		return previewBorderStyle.
			Width(p.width).
			Height(p.height).
			Align(lipgloss.Center, lipgloss.Center).
			Render(previewEmptyStyle.Render("No intent selected"))
	}

	title := previewTitleStyle.Render(p.title)
	scrollInfo := fmt.Sprintf(" %d%% ", int(p.viewport.ScrollPercent()*100))
	scrollStyle := lipgloss.NewStyle().Foreground(pal.TextMuted)

	header := lipgloss.JoinHorizontal(
		lipgloss.Top,
		title,
		lipgloss.NewStyle().Padding(0, 2).Render(scrollStyle.Render(scrollInfo)),
	)

	content := lipgloss.JoinVertical(lipgloss.Left, header, "", p.viewport.View())

	return previewBorderStyle.
		Width(p.width).
		Height(p.height).
		Render(content)
}

// HasContent returns true if the preview has content to display.
func (p PreviewPane) HasContent() bool {
	return p.content != ""
}
