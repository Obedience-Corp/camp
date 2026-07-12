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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
