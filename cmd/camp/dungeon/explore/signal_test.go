package explore

import (
	"errors"
	"os"
	"syscall"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// An interrupt or a termination from outside ends the explorer with the
// shell's code for that signal. main prints nothing for a CommandError with a
// code, so no "program was killed" reaches the terminal as if it had failed.
func TestExploreSignalExit(t *testing.T) {
	killed := errors.Join(tea.ErrProgramKilled, tea.ErrInterrupted)
	for _, tc := range []struct {
		name  string
		sig   os.Signal
		cause error
		code  int
	}{
		{"interrupt, program killed", os.Interrupt, killed, 130},
		{"interrupt, program quit first", syscall.SIGINT, nil, 130},
		{"termination, context cancelled", syscall.SIGTERM, tea.ErrProgramKilled, 143},
		{"termination, program quit first", syscall.SIGTERM, nil, 143},
	} {
		err := exploreSignalExit("camp dungeon explore", tc.sig, tc.cause)
		// main asserts the type directly, so the error must be the
		// CommandError itself, not something that wraps one.
		exit, ok := err.(*camperrors.CommandError)
		if !ok || exit.ExitCode != tc.code || exit.Stderr != "" {
			t.Errorf("%s: exit = %#v, want code %d and no text", tc.name, err, tc.code)
			continue
		}
		if tc.cause != nil && !errors.Is(err, tea.ErrProgramKilled) {
			t.Errorf("%s: the cause is lost: %v", tc.name, err)
		}
	}
	if err := exploreSignalExit("camp dungeon explore", nil, tea.ErrProgramKilled); err != nil {
		t.Fatalf("no signal: %v", err)
	}
}
