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
		// A command that returns a CommandError DIRECTLY is propagating a
		// child process's exit code (camp sync, doctor, clone, run), and has
		// already said whatever needed saying; printing again would be
		// redundant.
		//
		// This is a type assertion, not errors.As, and the difference is
		// load-bearing. Remote failures create a CommandError deep in the chain
		// (remote.Run wraps a non-zero ssh exit), and camp then wraps THAT with
		// the sentence the operator actually needs: which machine, which
		// campaign, and which diagnostic to run next. Matching anywhere in the
		// chain discarded all of it and exited silently with ssh's code, so a
		// failed hop printed nothing at all.
		var cmdErr *camperrors.CommandError
		if ok := errors.As(err, &cmdErr); ok && cmdErr.ExitCode != 0 && error(cmdErr) == err {
			os.Exit(cmdErr.ExitCode)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
