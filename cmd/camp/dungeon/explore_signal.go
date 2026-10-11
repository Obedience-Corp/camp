package dungeon

import (
	"os"
	"os/signal"
	"syscall"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// exploreSignals notes an interrupt or a termination sent to the process
// while the explorer owns the terminal. Bubble Tea and the command context
// both act on the same signal and race to end the program, one with an
// error and one without, so neither says which signal it was.
type exploreSignals struct {
	ch chan os.Signal
}

func watchExploreSignals() exploreSignals {
	w := exploreSignals{ch: make(chan os.Signal, 1)}
	signal.Notify(w.ch, os.Interrupt, syscall.SIGTERM)
	return w
}

// stop ends the watch and returns the signal that arrived, or nil.
func (w exploreSignals) stop() os.Signal {
	signal.Stop(w.ch)
	select {
	case sig := <-w.ch:
		return sig
	default:
		return nil
	}
}

// exploreSignalExit is how the explorer ends when sig stopped it: with the
// shell's code for a signal, 128 plus its number, and nothing printed. Being
// stopped from outside is not a failure of the command. docs/exit-codes.md
// lists these codes under Signals. It returns nil when sig is nil.
func exploreSignalExit(command string, sig os.Signal, cause error) error {
	number, ok := sig.(syscall.Signal)
	if !ok {
		return nil
	}
	return camperrors.NewCommand(command, 128+int(number), "", cause)
}
