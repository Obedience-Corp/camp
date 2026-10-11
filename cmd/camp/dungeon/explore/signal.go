package explore

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// exploreSignals notes an interrupt or a termination sent to the process
// while the terminal branch of the command runs. The command context ends
// the work on either signal and does not say which one it was.
type exploreSignals struct {
	ch chan os.Signal
}

// exploreUnderSignals runs the terminal branch of the command under a signal
// watch. The watch starts before the camp is read, so a signal during
// startup ends the command the same way as one that arrives while the feed
// is open: with the shell's code for that signal and nothing printed. JSON
// mode keeps its own error contract and is not watched.
func exploreUnderSignals(cmd *cobra.Command, run func() error) error {
	if exploreJSONMode(cmd) {
		return run()
	}
	signals := watchExploreSignals()
	err := run()
	if exit := exploreSignalExit(cmd.CommandPath(), signals.stop(), err); exit != nil {
		return exit
	}
	return err
}

// exploreProgramOptions are the Bubble Tea options the explorer runs with.
// Bubble Tea's own signal handler stays off. It hands a signal to the event
// loop with a bare channel send, and the command context, cancelled by the
// same signal, can end the loop first; shutdown then waits on that send for
// good, with the terminal still in raw mode on the alternate screen. The
// context alone ends the program. ctrl+c is unaffected: in raw mode it is a
// key, not a signal.
func exploreProgramOptions(ctx context.Context) []tea.ProgramOption {
	return []tea.ProgramOption{tea.WithContext(ctx), tea.WithAltScreen(), tea.WithoutSignalHandler()}
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
