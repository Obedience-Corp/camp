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
	helpKey     lipgloss.TerminalColor // Navigation key labels (up, down, etc.)
	helpDesc    lipgloss.TerminalColor // Navigation descriptions
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
		// Adaptive: ThemeCharm with only Help style fixes
		return buildAdaptiveTheme()
	}
}

// buildAdaptiveTheme creates a theme based on ThemeCharm (huh's default)
// but with brighter Help and Placeholder styles for better visibility.
func buildAdaptiveTheme() *huh.Theme {
	t := huh.ThemeCharm()

	// Fix dim Help styles (navigation hints)
	helpKey := lipgloss.AdaptiveColor{Light: "240", Dark: "250"}
	helpDesc := lipgloss.AdaptiveColor{Light: "243", Dark: "246"}

	t.Help.ShortKey = t.Help.ShortKey.Foreground(helpKey)
	t.Help.ShortDesc = t.Help.ShortDesc.Foreground(helpDesc)
	t.Help.ShortSeparator = t.Help.ShortSeparator.Foreground(helpDesc)
	t.Help.FullKey = t.Help.FullKey.Foreground(helpKey)
	t.Help.FullDesc = t.Help.FullDesc.Foreground(helpDesc)
	t.Help.FullSeparator = t.Help.FullSeparator.Foreground(helpDesc)

	// Fix dim placeholder text (ThemeCharm uses 238 dark/248 light which is too dim)
	placeholder := lipgloss.AdaptiveColor{Light: "240", Dark: "252"}
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(placeholder)
	t.Blurred.TextInput.Placeholder = t.Blurred.TextInput.Placeholder.Foreground(placeholder)

	return t
}

func darkPalette() palette {
	return palette{
		title:       lipgloss.Color("255"), // Bright white
		description: lipgloss.Color("252"), // Light grey
		focus:       lipgloss.Color("42"),  // Green
		selected:    lipgloss.Color("42"),  // Green
		option:      lipgloss.Color("252"), // Light grey
		placeholder: lipgloss.Color("248"), // Medium-light grey (brighter for visibility)
		errorColor:  lipgloss.Color("196"), // Red
		helpKey:     lipgloss.Color("252"), // Light grey for nav keys
		helpDesc:    lipgloss.Color("248"), // Medium-light grey for nav descriptions
	}
}

func lightPalette() palette {
	return palette{
		title:       lipgloss.Color("232"), // Near black
		description: lipgloss.Color("238"), // Dark grey
		focus:       lipgloss.Color("28"),  // Dark green
		selected:    lipgloss.Color("28"),  // Dark green
		option:      lipgloss.Color("235"), // Dark grey
		placeholder: lipgloss.Color("236"), // Darker grey (better contrast on light backgrounds)
		errorColor:  lipgloss.Color("124"), // Dark red
		helpKey:     lipgloss.Color("240"), // Dark grey for nav keys
		helpDesc:    lipgloss.Color("243"), // Medium-dark grey for nav descriptions
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
		helpKey:     lipgloss.Color("255"), // Bright white for nav keys
		helpDesc:    lipgloss.Color("252"), // Light grey for nav descriptions
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

	// Style help (navigation hints: up, down, filter, enter, submit)
	t.Help.ShortKey = t.Help.ShortKey.Foreground(p.helpKey)
	t.Help.ShortDesc = t.Help.ShortDesc.Foreground(p.helpDesc)
	t.Help.ShortSeparator = t.Help.ShortSeparator.Foreground(p.helpDesc)
	t.Help.FullKey = t.Help.FullKey.Foreground(p.helpKey)
	t.Help.FullDesc = t.Help.FullDesc.Foreground(p.helpDesc)
	t.Help.FullSeparator = t.Help.FullSeparator.Foreground(p.helpDesc)

	return t
}
