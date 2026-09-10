// Confirm demo binary for PTY verification of the shared huh theme. Driven by tests/tui/confirm_pty.py; not a user-facing command.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/huh"
)

func main() {
	var promote bool
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Promote the completed workitem?").
			Affirmative("Promote").Negative("Skip").Value(&promote),
	))
	if err := theme.RunForm(context.Background(), form); err != nil {
		if theme.IsCancelled(err) {
			fmt.Fprintln(os.Stderr, "CANCELLED")
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "RESULT:%v\n", promote)
}
