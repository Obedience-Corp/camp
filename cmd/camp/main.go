package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	// bginit must initialize before the bubbletea subtree under the command
	// packages; its path keeps it first under gofmt.
	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	_ "github.com/Obedience-Corp/camp/internal/bginit"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := Execute(ctx); err != nil {
		if code, silent := silentExit(err); silent {
			os.Exit(code)
		}
		// A staging-guard refusal is the one moment this package spends the
		// user's attention, so it prints the full report (what was found,
		// that nothing was staged, and the ways forward) in place of the
		// one-line error, rather than in addition to it.
		if cmdutil.RenderGuardRefusal(os.Stderr, err, ExecutedCommandPath()) {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
