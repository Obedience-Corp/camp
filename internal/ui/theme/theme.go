// Package theme provides theming for huh forms and TUI elements.
package theme

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// ThemeName represents a color theme name.
type ThemeName string

const (
	// ThemeAdaptive uses huh's default theme which auto-adapts to terminal.
	ThemeAdaptive ThemeName = "adaptive"
	// ThemeLight is optimized for light terminal backgrounds.
	ThemeLight ThemeName = "light"
	// ThemeDark is optimized for dark terminal backgrounds.
	ThemeDark ThemeName = "dark"
	// ThemeHighContrast provides maximum visibility.
	ThemeHighContrast ThemeName = "high-contrast"
)

// ValidThemes lists all valid theme names.
var ValidThemes = []ThemeName{ThemeAdaptive, ThemeLight, ThemeDark, ThemeHighContrast}

// IsValidTheme returns true if the theme name is valid.
func IsValidTheme(name string) bool {
	switch ThemeName(name) {
	case ThemeAdaptive, ThemeLight, ThemeDark, ThemeHighContrast:
		return true
	}
	return false
}

// palette holds colors for a theme.
type palette struct {
	title       lipgloss.TerminalColor
	description lipgloss.TerminalColor
	focus       lipgloss.TerminalColor
	selected    lipgloss.TerminalColor
	option      lipgloss.TerminalColor
	placeholder lipgloss.TerminalColor
	errorColor  lipgloss.TerminalColor
}

// GetTheme returns a huh.Theme for the given theme name.
func GetTheme(name ThemeName) *huh.Theme {
	switch name {
	case ThemeLight:
		return buildTheme(lightPalette())
	case ThemeDark:
		return buildTheme(darkPalette())
	case ThemeHighContrast:
		return buildTheme(highContrastPalette())
	default:
		// Adaptive: use huh's base theme
		return huh.ThemeBase()
	}
}

func darkPalette() palette {
	return palette{
		title:       lipgloss.Color("255"), // Bright white
		description: lipgloss.Color("252"), // Light grey
		focus:       lipgloss.Color("42"),  // Green
		selected:    lipgloss.Color("42"),  // Green
		option:      lipgloss.Color("252"), // Light grey
		placeholder: lipgloss.Color("245"), // Medium grey
		errorColor:  lipgloss.Color("196"), // Red
	}
}

func lightPalette() palette {
	return palette{
		title:       lipgloss.Color("232"), // Near black
		description: lipgloss.Color("238"), // Dark grey
		focus:       lipgloss.Color("28"),  // Dark green
		selected:    lipgloss.Color("28"),  // Dark green
		option:      lipgloss.Color("235"), // Dark grey
		placeholder: lipgloss.Color("241"), // Medium grey
		errorColor:  lipgloss.Color("124"), // Dark red
	}
}

func highContrastPalette() palette {
	return palette{
		title:       lipgloss.Color("255"), // Bright white
		description: lipgloss.Color("255"), // Bright white
		focus:       lipgloss.Color("46"),  // Bright green
		selected:    lipgloss.Color("46"),  // Bright green
		option:      lipgloss.Color("255"), // Bright white
		placeholder: lipgloss.Color("252"), // Light grey
		errorColor:  lipgloss.Color("196"), // Bright red
	}
}

func buildTheme(p palette) *huh.Theme {
	t := huh.ThemeBase()

	// Style focused elements
	t.Focused.Title = t.Focused.Title.Foreground(p.title).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(p.description)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(p.focus)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(p.selected)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(p.option)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(p.errorColor)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(p.placeholder)
	t.Focused.TextInput.Text = t.Focused.TextInput.Text.Foreground(p.title)

	// Style blurred elements (non-focused fields in multi-field forms)
	t.Blurred.Title = t.Blurred.Title.Foreground(p.placeholder)
	t.Blurred.Description = t.Blurred.Description.Foreground(p.placeholder)
	t.Blurred.SelectSelector = t.Blurred.SelectSelector.Foreground(p.placeholder)
	t.Blurred.SelectedOption = t.Blurred.SelectedOption.Foreground(p.placeholder)
	t.Blurred.UnselectedOption = t.Blurred.UnselectedOption.Foreground(p.placeholder)
	t.Blurred.TextInput.Placeholder = t.Blurred.TextInput.Placeholder.Foreground(p.placeholder)
	t.Blurred.TextInput.Text = t.Blurred.TextInput.Text.Foreground(p.placeholder)

	return t
}
