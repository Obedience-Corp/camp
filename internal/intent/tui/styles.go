package tui

import (
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/obediencecorp/camp/internal/ui/theme"
)

var (
	glamourStyle     string
	glamourStyleOnce sync.Once
)

// initGlamourStyle detects and caches the glamour style.
// Call this once at TUI startup with the user's theme preference.
// This avoids the slow OSC terminal query on every render.
func initGlamourStyle(themeName string) {
	glamourStyleOnce.Do(func() {
		switch themeName {
		case "dark":
			glamourStyle = "dark"
		case "light":
			glamourStyle = "light"
		case "high-contrast":
			glamourStyle = "dark" // high-contrast uses dark base
		default: // "adaptive" or empty
			// Detect once - this is the slow operation
			output := termenv.NewOutput(os.Stdout)
			if output.HasDarkBackground() {
				glamourStyle = "dark"
			} else {
				glamourStyle = "light"
			}
		}
	})
}

// renderMarkdown renders content with glamour using the cached style.
// Must call initGlamourStyle() before first use.
func renderMarkdown(content string, width int) string {
	// Fallback if not initialized
	if glamourStyle == "" {
		glamourStyle = "dark"
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath(glamourStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content
	}

	rendered, err := renderer.Render(content)
	if err != nil {
		return content
	}

	return strings.TrimSpace(rendered)
}

// pal is the TUI color palette for adaptive theming.
var pal = theme.TUI()

// Style definitions for the Intent Explorer TUI.
var (
	// Title styling
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(pal.Accent)

	// Group header styles
	groupHeaderStyle = lipgloss.NewStyle().
				Foreground(pal.AccentAlt)

	groupHeaderSelectedStyle = lipgloss.NewStyle().
					Background(pal.BgSelected).
					Bold(true).
					Foreground(pal.AccentAlt)

	// Intent row styles
	intentRowStyle = lipgloss.NewStyle().
			PaddingLeft(4)

	intentRowSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(4).
				Background(pal.BgSelected).
				Bold(true)

	// Intent field styles
	intentTitleStyle = lipgloss.NewStyle().
				Foreground(pal.TextPrimary)

	intentTypeStyle = lipgloss.NewStyle().
			Foreground(pal.TextSecondary)

	intentDateStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)

	intentConceptStyle = lipgloss.NewStyle().
				Foreground(pal.Warning)

	// Help bar style
	helpStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)

	// Error style
	errorStyle = lipgloss.NewStyle().
			Foreground(pal.Error)

	// Success style
	successStyle = lipgloss.NewStyle().
			Foreground(pal.Success)

	// Cursor indicator
	cursorIndicator = "›"
	noCursor        = " "

	// Filter pill styles
	filterPillStyle = lipgloss.NewStyle().
			Background(pal.BgSelected).
			Foreground(pal.TextPrimary).
			Padding(0, 1)

	filterPillActiveStyle = lipgloss.NewStyle().
				Background(pal.Accent).
				Foreground(pal.TextPrimary).
				Padding(0, 1).
				Bold(true)

	// Checkbox styles for multi-select
	checkboxCheckedStyle = lipgloss.NewStyle().
				Foreground(pal.Success)

	checkboxUncheckedStyle = lipgloss.NewStyle().
				Foreground(pal.TextMuted)

	// Selection count badge
	selectionCountStyle = lipgloss.NewStyle().
				Background(pal.Accent).
				Foreground(pal.TextPrimary).
				Padding(0, 1).
				Bold(true)
)
