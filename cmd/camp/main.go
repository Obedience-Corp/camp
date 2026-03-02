package main

import (
	"errors"
	"fmt"
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func main() {
	if err := Execute(); err != nil {
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
