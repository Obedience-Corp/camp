package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	// bginit must initialize before the bubbletea subtree under the command
	// packages; its path keeps it first under gofmt.
	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	_ "github.com/Obedience-Corp/camp/internal/bginit"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := Execute(ctx); err != nil {
		// If a child process exited with a specific code (e.g. camp run),
		// propagate that code without printing a redundant error message.
		var cmdErr *camperrors.CommandError
		if errors.As(err, &cmdErr) && cmdErr.ExitCode != 0 {
			os.Exit(cmdErr.ExitCode)
		}
		// A staging-guard refusal is the one moment this package spends the
		// user's attention, so it prints the full report (what was found,
		// that nothing was staged, and the ways forward) in place of the
		// one-line error, rather than in addition to it.
		if cmdutil.RenderGuardRefusal(os.Stderr, err, "camp commit") {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
