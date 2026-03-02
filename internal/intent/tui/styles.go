package tui

import (
	"os"
	"strings"
	"sync"

	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var (
	glamourStyle     string
	glamourStyleOnce sync.Once
)

// InitGlamourStyle detects and caches the glamour style.
// Call this once at TUI startup with the user's theme preference.
// This avoids the slow OSC terminal query on every render.
func InitGlamourStyle(themeName string) {
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
// Exported for use by explorer subpackage.
var (
	// Title styling
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(pal.Accent)

	// Group header styles
	GroupHeaderStyle = lipgloss.NewStyle().
				Foreground(pal.AccentAlt)

	GroupHeaderSelectedStyle = lipgloss.NewStyle().
					Background(pal.BgSelected).
					Bold(true).
					Foreground(pal.AccentAlt)

	// Dungeon parent header style (slightly muted, distinct from normal groups)
	DungeonHeaderStyle = lipgloss.NewStyle().
				Foreground(pal.TextMuted)

	// Intent row styles
	IntentRowStyle = lipgloss.NewStyle().
			PaddingLeft(4)

	IntentRowSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(4).
				Background(pal.BgSelected).
				Bold(true)

	// Intent field styles
	IntentTitleStyle = lipgloss.NewStyle().
				Foreground(pal.TextPrimary)

	IntentTypeStyle = lipgloss.NewStyle().
			Foreground(pal.TextSecondary)

	IntentDateStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)

	IntentConceptStyle = lipgloss.NewStyle().
				Foreground(pal.Warning)

	// Help bar style
	HelpStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)

	// Error style
	ErrorStyle = lipgloss.NewStyle().
			Foreground(pal.Error)

	// Success style
	SuccessStyle = lipgloss.NewStyle().
			Foreground(pal.Success)

	// Cursor indicator
	CursorIndicator = ">"
	NoCursor        = " "

	// Filter pill styles
	FilterPillStyle = lipgloss.NewStyle().
			Background(pal.BgSelected).
			Foreground(pal.TextPrimary).
			Padding(0, 1)

	FilterPillActiveStyle = lipgloss.NewStyle().
				Background(pal.Accent).
				Foreground(pal.TextPrimary).
				Padding(0, 1).
				Bold(true)

	// Checkbox styles for multi-select
	CheckboxCheckedStyle = lipgloss.NewStyle().
				Foreground(pal.Success)

	CheckboxUncheckedStyle = lipgloss.NewStyle().
				Foreground(pal.TextMuted)

	// Line numbers in explorer list
	LineNumberStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)

	// Selection count badge
	SelectionCountStyle = lipgloss.NewStyle().
				Background(pal.Accent).
				Foreground(pal.TextPrimary).
				Padding(0, 1).
				Bold(true)
)
