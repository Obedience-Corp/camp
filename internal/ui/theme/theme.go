// Package theme provides theming for huh forms and TUI elements.
package theme

import (
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Obedience-Corp/obey-shared/brand"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/config"
)

var plainOverride atomic.Bool

// ThemeName represents a color theme name.
type ThemeName string

const (
	// ThemeAdaptive uses huh's default theme which auto-adapts to terminal.
	ThemeAdaptive ThemeName = ThemeName(config.ThemeNameAdaptive)
	// ThemeLight is optimized for light terminal backgrounds.
	ThemeLight ThemeName = ThemeName(config.ThemeNameLight)
	// ThemeDark is optimized for dark terminal backgrounds.
	ThemeDark ThemeName = ThemeName(config.ThemeNameDark)
	// ThemeHighContrast provides maximum visibility.
	ThemeHighContrast ThemeName = ThemeName(config.ThemeNameHighContrast)
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

// GetTheme returns a huh.Theme for the given theme name.
func GetTheme(name ThemeName) *huh.Theme {
	palette := resolvePalette(name, currentCapabilities())
	if palette.Mode == brand.ModeLight {
		lipgloss.SetHasDarkBackground(false)
	} else if palette.Mode != brand.ModePlain {
		lipgloss.SetHasDarkBackground(true)
	}
	return buildTheme(palette)
}

// SetNoColor records the command-level no-color decision for shared palette
// resolution. Lip Gloss is configured separately by the parent ui package.
func SetNoColor(noColor bool) {
	plainOverride.Store(noColor)
}

// CurrentPalette resolves the adaptive shared brand palette for custom TUI
// components. Consumers should use semantic roles from this palette instead of
// introducing another product-local color table.
func CurrentPalette() brand.Palette {
	return resolvePalette(ThemeAdaptive, currentCapabilities())
}

func resolvePalette(name ThemeName, capabilities brand.Capabilities) brand.Palette {
	if plainOverride.Load() {
		return brand.Resolve(brand.ModePlain, capabilities)
	}
	return brand.Resolve(brand.ParseMode(string(name)), capabilities)
}

func currentCapabilities() brand.Capabilities {
	profile := termenv.EnvColorProfile()
	depth := brand.ColorTrueColor
	switch profile {
	case termenv.Ascii:
		depth = brand.ColorNone
	case termenv.ANSI:
		depth = brand.ColorANSI16
	case termenv.ANSI256:
		depth = brand.ColorANSI256
	}

	isTTY := term.IsTerminal(int(os.Stdout.Fd())) || term.IsTerminal(int(os.Stderr.Fd()))
	capabilities := brand.EnvironmentCapabilities(isTTY, depth)
	if dark, ok := backgroundFromColorFGBG(os.Getenv("COLORFGBG")); ok {
		capabilities.BackgroundKnown = true
		capabilities.DarkBackground = dark
	}
	return capabilities
}

func backgroundFromColorFGBG(value string) (bool, bool) {
	fields := strings.Split(value, ";")
	if len(fields) < 2 {
		return false, false
	}
	background, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		return false, false
	}
	return background <= 8 && background != 7, true
}

func color(value string) lipgloss.TerminalColor {
	if value == "" {
		// Plain mode is rendered through Lip Gloss's Ascii profile. Keep a
		// non-nil neutral adaptive token for components that still apply a
		// style to whitespace; Ascii suppresses it, while tests that switch
		// to TrueColor still receive a real background SGR sequence.
		return lipgloss.AdaptiveColor{Light: "255", Dark: "236"}
	}
	return lipgloss.Color(value)
}

func buildTheme(p brand.Palette) *huh.Theme {
	t := huh.ThemeBase()
	if p.Mode == brand.ModePlain {
		return t
	}

	title := color(p.TextPrimary)
	description := color(p.TextMuted)
	focus := color(p.Focus)
	selected := color(p.Accent)
	option := color(p.TextPrimary)
	placeholder := color(p.TextMuted)
	helpKey := color(p.TextPrimary)
	helpDesc := color(p.TextMuted)
	errorColor := color(p.StatusError)

	// Style focused elements
	t.Focused.Title = t.Focused.Title.Foreground(title).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(description)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(focus)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(selected)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(option)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(errorColor)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(placeholder)
	t.Focused.TextInput.Text = t.Focused.TextInput.Text.Foreground(title)

	// Style blurred elements (non-focused fields in multi-field forms)
	t.Blurred.Title = t.Blurred.Title.Foreground(placeholder)
	t.Blurred.Description = t.Blurred.Description.Foreground(placeholder)
	t.Blurred.SelectSelector = t.Blurred.SelectSelector.Foreground(placeholder)
	t.Blurred.SelectedOption = t.Blurred.SelectedOption.Foreground(placeholder)
	t.Blurred.UnselectedOption = t.Blurred.UnselectedOption.Foreground(placeholder)
	t.Blurred.TextInput.Placeholder = t.Blurred.TextInput.Placeholder.Foreground(placeholder)
	t.Blurred.TextInput.Text = t.Blurred.TextInput.Text.Foreground(placeholder)

	// Style help (navigation hints: up, down, filter, enter, submit)
	t.Help.ShortKey = t.Help.ShortKey.Foreground(helpKey)
	t.Help.ShortDesc = t.Help.ShortDesc.Foreground(helpDesc)
	t.Help.ShortSeparator = t.Help.ShortSeparator.Foreground(helpDesc)
	t.Help.FullKey = t.Help.FullKey.Foreground(helpKey)
	t.Help.FullDesc = t.Help.FullDesc.Foreground(helpDesc)
	t.Help.FullSeparator = t.Help.FullSeparator.Foreground(helpDesc)

	return t
}

// TUIPalette provides shared semantic colors for custom TUI components
// (non-huh). The values are resolved once from the current terminal
// capabilities, including adaptive, light, high-contrast, and plain modes.
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

// TUI returns the shared adaptive TUI palette.
// Use this for all custom TUI components to ensure consistent theming.
func TUI() TUIPalette {
	p := CurrentPalette()
	return TUIPalette{
		Accent:        color(p.Accent),
		AccentAlt:     color(p.AccentHighlight),
		Success:       color(p.StatusSuccess),
		Warning:       color(p.StatusWarning),
		Error:         color(p.StatusError),
		TextPrimary:   color(p.TextPrimary),
		TextSecondary: color(p.TextMuted),
		TextMuted:     color(p.TextMuted),
		TextDim:       color(p.TextMuted),
		BgSelected:    color(p.SurfaceRaised),
		BgOverlay:     color(p.SurfaceBase),
		Border:        color(p.Border),
		BorderFocus:   color(p.Focus),
	}
}
