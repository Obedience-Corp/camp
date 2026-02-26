package theme

import (
	"context"
	"os"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/config"
)

// redirectToTTY opens /dev/tty and configures the form to render there when
// stdout is not a terminal (e.g., inside command substitution). This allows
// interactive TUI pickers to display while the data output goes to stdout
// for capture. Returns a cleanup function that must be deferred.
func redirectToTTY(form *huh.Form) (*huh.Form, func()) {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return form, func() {}
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return form, func() {}
	}
	return form.WithOutput(tty).WithInput(tty), func() { tty.Close() }
}

// RunForm runs a huh form with the configured theme applied.
// It loads the theme from global config and applies it before running.
func RunForm(ctx context.Context, form *huh.Form) error {
	form, cleanup := redirectToTTY(form)
	defer cleanup()

	cfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		return form.WithTheme(GetTheme(ThemeAdaptive)).Run()
	}

	themeName := ThemeName(cfg.TUI.Theme)
	if themeName == "" {
		themeName = ThemeAdaptive
	}

	return form.WithTheme(GetTheme(themeName)).Run()
}

// RunFormAccessible runs a huh form with accessible mode enabled.
// This is useful for screen readers and other accessibility tools.
func RunFormAccessible(ctx context.Context, form *huh.Form) error {
	form, cleanup := redirectToTTY(form)
	defer cleanup()

	cfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		return form.WithTheme(GetTheme(ThemeAdaptive)).WithAccessible(true).Run()
	}

	themeName := ThemeName(cfg.TUI.Theme)
	if themeName == "" {
		themeName = ThemeAdaptive
	}

	return form.WithTheme(GetTheme(themeName)).WithAccessible(true).Run()
}

// IsCancelled returns true if the error indicates user cancellation.
func IsCancelled(err error) bool {
	return err == huh.ErrUserAborted
}
