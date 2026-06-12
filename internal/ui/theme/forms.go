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
// It resolves the effective theme (global config plus any campaign-local
// override) and applies it before running.
func RunForm(ctx context.Context, form *huh.Form) error {
	form, cleanup := redirectToTTY(form)
	defer cleanup()

	return form.WithTheme(GetTheme(ThemeName(config.EffectiveTheme(ctx)))).Run()
}

// RunFormAccessible runs a huh form with accessible mode enabled.
// This is useful for screen readers and other accessibility tools.
func RunFormAccessible(ctx context.Context, form *huh.Form) error {
	form, cleanup := redirectToTTY(form)
	defer cleanup()

	return form.WithTheme(GetTheme(ThemeName(config.EffectiveTheme(ctx)))).WithAccessible(true).Run()
}

// IsCancelled returns true if the error indicates user cancellation.
func IsCancelled(err error) bool {
	return err == huh.ErrUserAborted
}
