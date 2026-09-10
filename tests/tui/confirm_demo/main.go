// Confirm demo binary for PTY verification of the fresh merged-branch backstop
// prompt. Driven by tests/tui/confirm_pty.py; not a user-facing command.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Obedience-Corp/camp/internal/commands/fresh"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

func main() {
	var promote bool
	form := fresh.NewMergedPromoteForm(
		`Workitem "Settings: a row is a setting" had a merged branch and is still active. Promote to completed?`,
		"branch feature/obey-voice-settings-rows (merged)",
		&promote,
	)
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
