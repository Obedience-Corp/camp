package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := Execute(); err != nil {
		// If a child process exited with a specific code (e.g. camp run),
		// propagate that code without printing a redundant error message.
		var exitErr *CommandExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
