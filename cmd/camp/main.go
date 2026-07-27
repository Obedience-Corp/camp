package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	// bginit must initialize before the bubbletea subtree under the command
	// packages; its path keeps it first under gofmt.
	_ "github.com/Obedience-Corp/camp/internal/bginit"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := Execute(ctx); err != nil {
		if code, silent := silentExit(err); silent {
			os.Exit(code)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
