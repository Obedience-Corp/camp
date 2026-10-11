package main

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/jsoncontract"
)

// The failed-job notice is a line of text on stderr, where a JSON caller
// reads an error envelope. A command that prints JSON gets no notice, however
// it came to print JSON: the typed flag, a --format json alias, or a rule of
// its own such as a pipe.
func TestSuppressesNoticesWheneverTheCommandPrintsJSON(t *testing.T) {
	newCommand := func() *cobra.Command {
		cmd := &cobra.Command{Use: "feed"}
		cmd.Flags().Bool("json", false, "")
		cmd.Flags().String("format", "table", "")
		return cmd
	}
	set := func(cmd *cobra.Command, flag, value string) *cobra.Command {
		t.Helper()
		if err := cmd.Flags().Set(flag, value); err != nil {
			t.Fatal(err)
		}
		return cmd
	}

	if suppressesNotices(newCommand()) {
		t.Fatal("a command printing text gets no notice")
	}
	if suppressesNotices(set(newCommand(), "format", "simple")) {
		t.Fatal("--format simple gets no notice")
	}
	if !suppressesNotices(set(newCommand(), "json", "true")) {
		t.Fatal("--json still gets the notice")
	}
	if !suppressesNotices(set(newCommand(), "format", "json")) {
		t.Fatal("--format json still gets the notice")
	}

	onPipe := true
	declaring := newCommand()
	jsoncontract.Declare(declaring, func() bool { return onPipe })
	if !suppressesNotices(declaring) {
		t.Fatal("a command that declared it prints JSON still gets the notice")
	}
	onPipe = false
	if suppressesNotices(declaring) {
		t.Fatal("a command that declared it prints text gets no notice")
	}
}
