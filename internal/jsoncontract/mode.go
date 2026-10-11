package jsoncontract

import (
	"sync"

	"github.com/spf13/cobra"
)

// declared holds, per command, the predicate that says an invocation prints
// JSON. The value is a func() bool.
var declared sync.Map

// Declare records how cmd decides it prints JSON. A command whose answer is
// more than a typed --json flag, such as one that prints JSON on a pipe,
// declares it so code that runs outside the command asks the same question
// the command does.
func Declare(cmd *cobra.Command, jsonRequested func() bool) {
	declared.Store(cmd, jsonRequested)
}

// Declared reports whether cmd declared that this invocation prints JSON. It
// is false for a command that declared nothing.
func Declared(cmd *cobra.Command) bool {
	decide, ok := declared.Load(cmd)
	return ok && decide.(func() bool)()
}
