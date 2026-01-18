package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Label renders text as a label (key name in key/value pairs)
func Label(text string) string {
	return labelStyle.Render(text)
}

// Value renders text as a value (bright white, bold)
func Value(text string, colors ...lipgloss.Color) string {
	if len(colors) > 0 && colors[0] != "" {
		return valueStyle.Foreground(colors[0]).Render(text)
	}
	return valueStyle.Render(text)
}

// Success renders success text (green)
func Success(text string) string {
	return successStyle.Render(text)
}

// Error renders error text (red)
func Error(text string) string {
	return errorStyle.Render(text)
}

// Warning renders warning text (yellow)
func Warning(text string) string {
	return warningStyle.Render(text)
}

// Info renders info text (blue)
func Info(text string) string {
	return infoStyle.Render(text)
}

// Dim renders secondary/metadata text (grey)
func Dim(text string) string {
	return dimStyle.Render(text)
}

// Accent renders accent text (cyan)
func Accent(text string) string {
	return accentStyle.Render(text)
}

// Category renders category text (purple)
func Category(text string) string {
	return categoryStyle.Render(text)
}

// Header renders a top-level header (H1)
func Header(text string) string {
	return headerStyle.Render(text)
}

// Subheader renders a section header (H2)
func Subheader(text string) string {
	return subheadStyle.Render(text)
}

// ColoredText renders text in a specific color
func ColoredText(text string, color lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(color).Render(text)
}

// BoldText renders bold text
func BoldText(text string) string {
	return lipgloss.NewStyle().Bold(true).Render(text)
}

// SuccessIcon returns a green checkmark
func SuccessIcon() string {
	return ColoredText("✓", SuccessColor)
}

// ErrorIcon returns a red X
func ErrorIcon() string {
	return ColoredText("✗", ErrorColor)
}

// WarningIcon returns a yellow warning symbol
func WarningIcon() string {
	return ColoredText("⚠", WarningColor)
}

// InfoIcon returns a blue info symbol
func InfoIcon() string {
	return ColoredText("ℹ", InfoColor)
}

// BulletIcon returns a bullet point
func BulletIcon() string {
	return ColoredText("•", DimColor)
}

// ArrowIcon returns an arrow
func ArrowIcon() string {
	return ColoredText("→", AccentColor)
}

// StateIcon returns an appropriate icon for a state
func StateIcon(state string) string {
	switch strings.ToLower(state) {
	case "success", "done", "completed", "created":
		return SuccessIcon()
	case "error", "failed", "removed":
		return ErrorIcon()
	case "warning", "skipped":
		return WarningIcon()
	case "info", "pending":
		return InfoIcon()
	default:
		return BulletIcon()
	}
}

// Separator returns a horizontal line
func Separator(width int) string {
	return Dim(strings.Repeat("─", width))
}

// KeyValue formats a key-value pair
func KeyValue(key, value string) string {
	return Label(key) + " " + Value(value)
}

// KeyValueColored formats a key-value pair with a colored value
func KeyValueColored(key, value string, color lipgloss.Color) string {
	return Label(key) + " " + Value(value, color)
}

// Indent adds indentation to text
func Indent(text string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}
