package tui

import (
	"strings"

	"github.com/Obedience-Corp/obey-shared/brand"
	brandlip "github.com/Obedience-Corp/obey-shared/brand/lipgloss"
	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// brandStyles adapts the shared fire theme for intent chrome.
// Resolved once at package init (same timing as pal / styles.go).
var brandStyles = brandlip.New(themePalette())

func themePalette() brand.Palette {
	// Use the same adaptive capability detection as theme.TUI() /
	// theme.CurrentPalette() (TTY, color depth, NO_COLOR, dumb TERM, etc.).
	return theme.CurrentPalette()
}

// Header renders a single-line brand chrome row:
//
//	▲ intent  ·  <title>                    <right>
//
// followed by a fire-toned horizontal rule.
func Header(title, right string, width int) string {
	if width < 20 {
		width = 20
	}
	left := brandStyles.Fire.Render(brand.CompactMark + " intent")
	if title != "" {
		left += brandStyles.Muted.Render("  ·  ") + brandStyles.Title.Render(title)
	}
	rightStyled := brandStyles.Muted.Render(right)
	gap := width - lipgloss.Width(left) - lipgloss.Width(rightStyled)
	if gap < 1 {
		gap = 1
	}
	row := left + strings.Repeat(" ", gap) + rightStyled
	return row + "\n" + brandlip.Rule(width, brandStyles)
}

// Footer renders a horizontal rule and dim key hints.
func Footer(hints string, width int) string {
	if width < 20 {
		width = 20
	}
	return brandlip.Rule(width, brandStyles) + "\n" +
		brandStyles.Footer.MaxWidth(width).Render(hints)
}

// StepPills renders a horizontal step indicator for multi-step flows.
// current is 0-based; steps are labels like Title, Type, Body.
func StepPills(steps []string, current int) string {
	if len(steps) == 0 {
		return ""
	}
	var parts []string
	for i, step := range steps {
		switch {
		case i < current:
			parts = append(parts, brandStyles.OK.Render("✓ "+step))
		case i == current:
			parts = append(parts, brandStyles.Fire.Render("▸ "+step))
		default:
			parts = append(parts, brandStyles.Muted.Render("· "+step))
		}
	}
	return strings.Join(parts, brandStyles.Muted.Render("  "))
}

// FocusCursor is the fire-colored selection caret used in lists.
func FocusCursor() string {
	return brandStyles.Fire.Render("▸")
}

// EmptyCursor pads the unselected list column to match FocusCursor width.
func EmptyCursor() string {
	return " "
}
