package vim

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/ui/theme"
)

// ViewConfig configures the editor view rendering.
type ViewConfig struct {
	// Styles
	NormalText    lipgloss.Style
	CursorBlock   lipgloss.Style // For normal mode cursor (inverted)
	CursorInsert  lipgloss.Style // For insert mode cursor position
	Selection     lipgloss.Style // For visual mode selection
	LineNumber    lipgloss.Style
	CommandLine   lipgloss.Style
	ShowLineNums  bool
}

// DefaultViewConfig returns default styles using the theme package for adaptive colors.
func DefaultViewConfig() ViewConfig {
	pal := theme.TUI()
	return ViewConfig{
		NormalText:   lipgloss.NewStyle(),
		CursorBlock:  lipgloss.NewStyle().Reverse(true),
		CursorInsert: lipgloss.NewStyle().Underline(true),
		Selection:    lipgloss.NewStyle().Background(pal.BgSelected),
		LineNumber:   lipgloss.NewStyle().Foreground(pal.TextMuted),
		CommandLine:  lipgloss.NewStyle().Foreground(pal.TextSecondary),
		ShowLineNums: true,
	}
}

// View renders the editor content with cursor and selection highlighting.
func (e *Editor) View(cfg ViewConfig) string {
	var b strings.Builder

	lines := e.buffer.Lines()
	cursor := e.buffer.Cursor()

	// Calculate visible range based on scroll offset
	startLine := e.scrollOffset
	endLine := min(startLine+e.height, len(lines))

	// Ensure we have at least some lines to show
	if endLine <= startLine {
		endLine = startLine + 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}

	// Get visual selection range if applicable
	var selStartOff, selEndOff int
	inVisual := e.state.IsVisual()
	if inVisual {
		selStartOff, selEndOff = e.state.VisualRange(e.buffer.CursorOffset())
	}

	for lineIdx := startLine; lineIdx < endLine; lineIdx++ {
		if lineIdx >= len(lines) {
			break
		}
		line := lines[lineIdx]

		// Optional line numbers
		if cfg.ShowLineNums {
			lineNum := lipgloss.NewStyle().Width(4).Align(lipgloss.Right).Render(
				strings.TrimSpace(cfg.LineNumber.Render(itoa(lineIdx+1) + " ")),
			)
			b.WriteString(lineNum)
		}

		// Render line content with cursor/selection highlighting
		renderedLine := e.renderLine(lineIdx, line, cursor, cfg, inVisual, selStartOff, selEndOff)

		// Apply soft wrapping to fit within editor width
		if e.width > 0 {
			wrapStyle := lipgloss.NewStyle().Width(e.width)
			renderedLine = wrapStyle.Render(renderedLine)
		}

		b.WriteString(renderedLine)

		if lineIdx < endLine-1 {
			b.WriteString("\n")
		}
	}

	// Pad with empty lines if content is shorter than height
	for i := endLine - startLine; i < e.height; i++ {
		b.WriteString("\n~")
	}

	return b.String()
}

// renderLine renders a single line with cursor/selection highlighting.
func (e *Editor) renderLine(lineIdx int, line string, cursor Position, cfg ViewConfig, inVisual bool, selStartOff, selEndOff int) string {
	lineStartOffset := e.lineStartOffset(lineIdx)

	// Handle empty line with cursor
	if len(line) == 0 {
		if lineIdx == cursor.Line {
			if e.state.Mode == ModeInsert {
				return cfg.CursorInsert.Render(" ")
			}
			return cfg.CursorBlock.Render(" ")
		}
		return " " // Empty line placeholder
	}

	var result strings.Builder

	for col, ch := range line {
		charOffset := lineStartOffset + col
		char := string(ch)

		isCursor := lineIdx == cursor.Line && col == cursor.Col
		isSelected := inVisual && charOffset >= selStartOff && charOffset <= selEndOff

		switch {
		case isCursor && e.state.Mode != ModeInsert:
			// Block cursor in normal/visual mode
			result.WriteString(cfg.CursorBlock.Render(char))
		case isSelected:
			// Visual selection
			result.WriteString(cfg.Selection.Render(char))
		default:
			result.WriteString(cfg.NormalText.Render(char))
		}
	}

	// Handle cursor at end of line in insert mode
	if e.state.Mode == ModeInsert && lineIdx == cursor.Line && cursor.Col >= len(line) {
		result.WriteString(cfg.CursorInsert.Render(" "))
	}

	return result.String()
}

// lineStartOffset calculates the absolute offset at the start of a line.
func (e *Editor) lineStartOffset(lineIdx int) int {
	offset := 0
	lines := e.buffer.Lines()
	for i := 0; i < lineIdx && i < len(lines); i++ {
		offset += len(lines[i]) + 1 // +1 for newline
	}
	return offset
}

// EnsureCursorVisible adjusts scroll offset to keep cursor visible.
func (e *Editor) EnsureCursorVisible() {
	cursor := e.buffer.Cursor()

	// Scroll up if cursor above viewport
	if cursor.Line < e.scrollOffset {
		e.scrollOffset = cursor.Line
	}

	// Scroll down if cursor below viewport
	if cursor.Line >= e.scrollOffset+e.height {
		e.scrollOffset = cursor.Line - e.height + 1
	}

	// Clamp scroll offset
	if e.scrollOffset < 0 {
		e.scrollOffset = 0
	}
}

// itoa is a simple int to string conversion.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
