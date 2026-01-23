package theme

import (
	"context"

	"github.com/charmbracelet/huh"

	"github.com/obediencecorp/camp/internal/config"
)

// RunForm runs a huh form with the configured theme applied.
// It loads the theme from global config and applies it before running.
func RunForm(ctx context.Context, form *huh.Form) error {
	cfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		// Use huh defaults on error
		return form.Run()
	}

	themeName := ThemeName(cfg.TUI.Theme)

	// Adaptive = let huh auto-detect terminal colors
	if themeName == "" || themeName == ThemeAdaptive {
		return form.Run()
	}

	return form.WithTheme(GetTheme(themeName)).Run()
}

// RunFormAccessible runs a huh form with accessible mode enabled.
// This is useful for screen readers and other accessibility tools.
func RunFormAccessible(ctx context.Context, form *huh.Form) error {
	cfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		// Use huh defaults on error
		return form.WithAccessible(true).Run()
	}

	themeName := ThemeName(cfg.TUI.Theme)

	// Adaptive = let huh auto-detect terminal colors
	if themeName == "" || themeName == ThemeAdaptive {
		return form.WithAccessible(true).Run()
	}

	return form.WithTheme(GetTheme(themeName)).WithAccessible(true).Run()
}

// IsCancelled returns true if the error indicates user cancellation.
func IsCancelled(err error) bool {
	return err == huh.ErrUserAborted
}
