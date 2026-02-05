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
//
// NOTE: Due to a bug in huh/bubbles, Blurred.TextInput.Placeholder doesn't
// work for fields that were never focused. The bubbles textarea's m.style
// pointer is initialized to point to a local variable in New(), not to
// m.BlurredStyle. Only Focus()/Blur() calls fix this, but fields that start
// blurred never get those calls. We work around this by:
// 1. Making focused placeholder bright (works correctly)
// 2. Using Description text instead of placeholder for important prompts
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

	// Make focused placeholder bright (this works correctly)
	t.Focused.TextInput.Placeholder = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "243", Dark: "250"})

	// Blurred placeholder styling doesn't work due to huh/bubbles bug,
	// but we set it anyway in case they fix it in the future
	t.Blurred.TextInput.Placeholder = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "248"})

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

// TUIPalette provides adaptive colors for custom TUI components (non-huh).
// All colors use lipgloss.AdaptiveColor for automatic light/dark adaptation.
type TUIPalette struct {
	// Primary colors
	Accent    lipgloss.TerminalColor // Primary accent (pink/magenta)
	AccentAlt lipgloss.TerminalColor // Secondary accent (blue)
	Success   lipgloss.TerminalColor // Success/positive (green)
	Warning   lipgloss.TerminalColor // Warning (orange/yellow)
	Error     lipgloss.TerminalColor // Error/danger (red)

	// Text colors
	TextPrimary   lipgloss.TerminalColor // Main text (titles, content)
	TextSecondary lipgloss.TerminalColor // Secondary text (types, badges)
	TextMuted     lipgloss.TerminalColor // Muted text (dates, help, separators)
	TextDim       lipgloss.TerminalColor // Very dim text (disabled, inactive)

	// Background colors
	BgSelected lipgloss.TerminalColor // Selected item background
	BgOverlay  lipgloss.TerminalColor // Modal/overlay background

	// Border colors
	Border      lipgloss.TerminalColor // Default border
	BorderFocus lipgloss.TerminalColor // Focused border
}

// TUI returns the global adaptive TUI palette.
// Use this for all custom TUI components to ensure consistent theming.
func TUI() TUIPalette {
	return TUIPalette{
		// Primary colors - visible on both light and dark
		Accent:    lipgloss.AdaptiveColor{Light: "205", Dark: "205"}, // Pink/magenta
		AccentAlt: lipgloss.AdaptiveColor{Light: "27", Dark: "110"},  // Blue
		Success:   lipgloss.AdaptiveColor{Light: "28", Dark: "82"},   // Green
		Warning:   lipgloss.AdaptiveColor{Light: "208", Dark: "214"}, // Orange
		Error:     lipgloss.AdaptiveColor{Light: "124", Dark: "196"}, // Red

		// Text colors - adjusted for contrast
		TextPrimary:   lipgloss.AdaptiveColor{Light: "232", Dark: "255"}, // Near black / bright white
		TextSecondary: lipgloss.AdaptiveColor{Light: "238", Dark: "250"}, // Dark grey / light grey
		TextMuted:     lipgloss.AdaptiveColor{Light: "243", Dark: "246"}, // Medium grey (visible!)
		TextDim:       lipgloss.AdaptiveColor{Light: "248", Dark: "242"}, // Light grey / medium grey

		// Background colors
		BgSelected: lipgloss.AdaptiveColor{Light: "254", Dark: "237"}, // Light grey / dark grey
		BgOverlay:  lipgloss.AdaptiveColor{Light: "255", Dark: "236"}, // White / very dark grey

		// Border colors
		Border:      lipgloss.AdaptiveColor{Light: "250", Dark: "240"}, // Light grey / medium grey
		BorderFocus: lipgloss.AdaptiveColor{Light: "205", Dark: "205"}, // Pink (matches accent)
	}
}
