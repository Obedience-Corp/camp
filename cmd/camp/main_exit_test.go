package main

import (
	"errors"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// silentExit mirrors main's decision so the rule is testable without spawning a
// process. Keep the condition here identical to main().
func silentExit(err error) (int, bool) {
	var cmdErr *camperrors.CommandError
	if ok := errors.As(err, &cmdErr); ok && cmdErr.ExitCode != 0 && error(cmdErr) == err {
		return cmdErr.ExitCode, true
	}
	return 0, false
}

func TestSilentExitOnlyForDirectlyReturnedCommandErrors(t *testing.T) {
	direct := camperrors.NewCommand("camp sync", 3, "", nil)

	t.Run("direct return propagates the code silently", func(t *testing.T) {
		code, silent := silentExit(direct)
		if !silent || code != 3 {
			t.Errorf("silentExit = (%d, %v), want (3, true)", code, silent)
		}
	})

	t.Run("a wrapped remote failure must PRINT, not vanish", func(t *testing.T) {
		// This is the shape a failed hop produces: ssh exits non-zero, remote.Run
		// turns that into a CommandError, and camp wraps it with the sentence the
		// operator needs. Matching anywhere in the chain threw all of it away and
		// exited silently with ssh's code, so the hop printed nothing at all.
		wrapped := camperrors.Wrapf(direct, "could not resolve %q on %s", "obey-campaign", "archdtop")
		if _, silent := silentExit(wrapped); silent {
			t.Error("a wrapped error was silenced; the operator would see an exit code and no message")
		}
	})

	t.Run("doubly wrapped is still printed", func(t *testing.T) {
		wrapped := camperrors.Wrapf(camperrors.Wrapf(direct, "resolve"), "run 'camp machine diagnose archdtop'")
		if _, silent := silentExit(wrapped); silent {
			t.Error("nested wrapping must not re-enable the silent path")
		}
	})

	t.Run("zero exit code is never silent", func(t *testing.T) {
		launchFailure := camperrors.NewCommand("camp run", 0, "", errors.New("exec failed"))
		if _, silent := silentExit(launchFailure); silent {
			t.Error("a launch failure has no exit code to propagate and must print")
		}
	})

	t.Run("an ordinary error prints", func(t *testing.T) {
		if _, silent := silentExit(errors.New("plain")); silent {
			t.Error("plain errors must print")
		}
	})
}
