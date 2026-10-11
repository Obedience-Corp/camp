//go:build unix

package dungeon

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestExploreSignalsNameTheSignalThatArrived(t *testing.T) {
	quiet := watchExploreSignals()
	if sig := quiet.stop(); sig != nil {
		t.Fatalf("a watch with no signal reported %v", sig)
	}
	for _, want := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		watch := watchExploreSignals()
		if err := syscall.Kill(os.Getpid(), want); err != nil {
			watch.stop()
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for len(watch.ch) == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if got := watch.stop(); got != want {
			t.Fatalf("watch reported %v, want %v", got, want)
		}
	}
}

// keepSignals holds a listener of the test's own for the two signals, so one
// sent to the test process can never take its default action, and reports
// when it has been delivered. os/signal fills every listener under one lock:
// once this one has the signal, so does every other.
func keepSignals(t *testing.T) (delivered func(syscall.Signal)) {
	t.Helper()
	own := make(chan os.Signal, 1)
	signal.Notify(own, syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Stop(own) })
	return func(sig syscall.Signal) {
		t.Helper()
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-own:
			if got != sig {
				t.Fatalf("delivered %v, sent %v", got, sig)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%v was never delivered", sig)
		}
	}
}

type signalProbe struct{ seen chan int }

type signalProbeMsg int

func (p signalProbe) Init() tea.Cmd { return nil }

func (p signalProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if n, ok := msg.(signalProbeMsg); ok {
		p.seen <- int(n)
	}
	return p, nil
}

func (p signalProbe) View() string { return "" }

// Bubble Tea's own handler turned a signal into a message with a bare channel
// send. The command context, cancelled by the same signal, could end the
// event loop first, and shutdown then waited on that send for good: about one
// signal in six left the explorer hung in raw mode on the alternate screen.
// With the options the explorer runs under, a signal does nothing to the
// program by itself, and a cancelled context always ends it.
func TestExploreProgramLeavesSignalsToTheCommand(t *testing.T) {
	deliver := keepSignals(t)
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		ctx, cancel := context.WithCancel(context.Background())
		probe := signalProbe{seen: make(chan int, 1)}
		opts := append(exploreProgramOptions(ctx), tea.WithInput(bytes.NewReader(nil)), tea.WithOutput(io.Discard))
		prog := tea.NewProgram(probe, opts...)
		done := make(chan error, 1)
		go func() {
			_, err := prog.Run()
			done <- err
		}()
		handled := func(n int) {
			t.Helper()
			go prog.Send(signalProbeMsg(n))
			select {
			case got := <-probe.seen:
				if got != n {
					t.Fatalf("after %v: handled message %d, want %d", sig, got, n)
				}
			case err := <-done:
				t.Fatalf("%v ended the program by itself: %v", sig, err)
			case <-time.After(10 * time.Second):
				t.Fatalf("after %v the program stopped handling messages", sig)
			}
		}
		handled(1)
		deliver(sig)
		// A handler of Bubble Tea's own would have queued its quit by now,
		// ahead of these.
		handled(2)
		handled(3)
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, tea.ErrProgramKilled) {
				t.Fatalf("after %v and a cancelled context: %v, want the program killed", sig, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("after %v the program did not end with its context", sig)
		}
	}
}

// The watch covers the whole terminal branch, startup included: a signal
// that lands while the camp is still being read ends the command like one
// that lands in the feed, whatever error the interrupted work returned.
func TestExploreUnderSignalsCoversStartup(t *testing.T) {
	deliver := keepSignals(t)
	terminal := exploreTerminal
	exploreTerminal = func() bool { return true }
	t.Cleanup(func() { exploreTerminal = terminal })
	cmd := &cobra.Command{Use: "explore"}
	cmd.Flags().Bool("json", false, "")
	interrupted := camperrors.Wrap(context.Canceled, "not in a camp directory")

	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		err := exploreUnderSignals(cmd, func() error {
			deliver(sig)
			return interrupted
		})
		exit, ok := err.(*camperrors.CommandError)
		if !ok || exit.ExitCode != 128+int(sig) || exit.Stderr != "" {
			t.Errorf("%v during startup: %#v, want exit %d and no text", sig, err, 128+int(sig))
		}
	}

	// With no signal the branch's own result comes back as it is.
	if err := exploreUnderSignals(cmd, func() error { return interrupted }); err != interrupted {
		t.Errorf("no signal: %v, want the error the branch returned", err)
	}
	if err := exploreUnderSignals(cmd, func() error { return nil }); err != nil {
		t.Errorf("no signal, no error: %v", err)
	}

	// JSON mode answers with its error envelope and is not watched.
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	err := exploreUnderSignals(cmd, func() error {
		deliver(syscall.SIGTERM)
		return interrupted
	})
	if err != interrupted {
		t.Errorf("--json with a signal: %v, want the branch's own error", err)
	}
}
