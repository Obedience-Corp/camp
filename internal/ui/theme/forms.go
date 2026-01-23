package theme

import (
	"context"

	"github.com/charmbracelet/huh"

	"github.com/obediencecorp/camp/internal/config"
)

// RunForm runs a huh form with the configured theme applied.
// It loads the theme from global config and applies it before running.
func RunForm(ctx context.Context, form *huh.Form) error {
	cfg, _ := config.LoadGlobalConfig(ctx)

	themeName := ThemeName(cfg.TUI.Theme)
	if themeName == "" {
		themeName = ThemeAdaptive
	}

	form = form.WithTheme(GetTheme(themeName))

	return form.Run()
}

// RunFormAccessible runs a huh form with accessible mode enabled.
// This is useful for screen readers and other accessibility tools.
func RunFormAccessible(ctx context.Context, form *huh.Form) error {
	cfg, _ := config.LoadGlobalConfig(ctx)

	themeName := ThemeName(cfg.TUI.Theme)
	if themeName == "" {
		themeName = ThemeAdaptive
	}

	form = form.WithTheme(GetTheme(themeName)).WithAccessible(true)

	return form.Run()
}

// IsCancelled returns true if the error indicates user cancellation.
func IsCancelled(err error) bool {
	return err == huh.ErrUserAborted
}
