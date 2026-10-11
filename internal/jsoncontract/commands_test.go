package jsoncontract_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Obedience-Corp/camp/cmd/camp/intent"
	"github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
)

// campTree is real commands that report through jsoncontract, under a root
// with the real root's output settings. Every row below is refused by the
// flag parser or by the argument check, before a command reads anything.
var campTree = func() *cobra.Command {
	root := &cobra.Command{Use: "camp", SilenceErrors: true, SilenceUsage: true}
	// idea declares GroupID planning, which Execute rejects unless the root
	// defines that group the way the real root does.
	root.AddGroup(&cobra.Group{ID: "planning", Title: "Planning Commands:"})
	root.AddCommand(intent.Cmd, workitem.NewWorkitemCommand())
	return root
}()

// refusalForm runs argv and reports whether the refusal came as the JSON
// envelope. The other form is the returned error with nothing on stderr.
func refusalForm(t *testing.T, argv string) (envelope bool) {
	t.Helper()
	args := strings.Fields(argv)
	origArgs := os.Args
	os.Args = append([]string{"camp"}, args...)
	var stderr bytes.Buffer
	campTree.SetErr(&stderr)
	campTree.SetOut(io.Discard)
	campTree.SetArgs(args)
	defer func() {
		os.Args = origArgs
		campTree.SetErr(nil)
		campTree.SetOut(nil)
		campTree.SetArgs(nil)
		campTree.SilenceErrors, campTree.SilenceUsage = true, true
		resetFlags(campTree)
	}()

	if err := campTree.Execute(); err == nil {
		t.Fatalf("camp %s was not refused", argv)
	}
	if stderr.Len() == 0 {
		return false
	}
	var got jsoncontract.ErrorEnvelope
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil || got.SchemaVersion == "" || got.Error.Message == "" {
		t.Fatalf("camp %s: stderr is neither empty nor one envelope: %q", argv, stderr.String())
	}
	return true
}

func resetFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if !strings.Contains(f.Value.Type(), "Slice") {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	})
	for _, child := range cmd.Commands() {
		resetFlags(child)
	}
}

// The JSON decision is shared by every command with a JSON contract. These
// are command lines other commands handled correctly before the decision
// stopped reading argv after a parse: value flags in every spelling, the
// --format json alias, an optional-value flag, and --json on either side of
// the argument pflag refuses. Each still gets the form it got.
func TestOtherCommandsKeepTheirRefusalForm(t *testing.T) {
	for argv, wantEnvelope := range map[string]bool{
		// Refused by the argument check, after the flags parsed.
		"idea list extra --json":                             true,
		"idea list --json extra":                             true,
		"idea list extra":                                    false,
		"idea list --json=false extra":                       false,
		"idea list --json=true extra":                        true,
		"idea list --status inbox --json extra":              true,
		"idea list -s inbox --json extra":                    true,
		"idea list -sinbox -n5 --json extra":                 true,
		"idea list --project=camp --json extra":              true,
		"idea list -a --json extra":                          true,
		"idea list -f json extra":                            true,
		"idea list --format=json extra":                      true,
		"idea list --format simple extra":                    false,
		"idea list --json=false --json extra":                true,
		"idea show --json":                                   true,
		"idea show":                                          false,
		"idea show -f json":                                  true,
		"idea show one two --json":                           true,
		"idea add one two three --campaign --json":           true,
		"idea add one two three --campaign":                  false,
		"workitem link --json":                               true,
		"workitem link --project camp --role related --json": true,
		"workitem link --project camp":                       false,
		"workitem link one two three --json=true":            true,
		"workitem priority demo --json":                      true,
		"workitem priority demo":                             false,
		"wi priority --json":                                 true,

		// Refused by the flag parser, which stops there.
		"idea list --json --bogus":                    true,
		"idea list --bogus --json":                    true,
		"idea list --bogus":                           false,
		"idea list --json=false --bogus":              false,
		"idea list --limit many --json":               true,
		"idea list --json --limit":                    true,
		"idea list -z --json":                         true,
		"idea list --json=maybe":                      true,
		"idea list --bogus --status inbox --json":     true,
		"idea list --bogus -s inbox --json":           true,
		"workitem link --json --role":                 true,
		"workitem link --bogus --project camp --json": true,
		"workitem priority --bogus --json":            true,
		"workitem priority --bogus":                   false,
	} {
		t.Run(argv, func(t *testing.T) {
			if got := refusalForm(t, argv); got != wantEnvelope {
				t.Fatalf("camp %s: envelope = %v, want %v", argv, got, wantEnvelope)
			}
		})
	}
}

// These are the command lines the decision used to get wrong, in any
// command: a --json that cobra did not read as the flag was taken for it,
// and the first --json in argv won over a later one. The form now follows
// what cobra parsed.
func TestAStrayJSONTokenDoesNotPickTheRefusalForm(t *testing.T) {
	for argv, wantEnvelope := range map[string]bool{
		// --json is the value of --project, -p, and --role.
		"idea list --project --json extra": false,
		"idea list -p --json extra":        false,
		"workitem link --role --json":      false,
		// --json follows the end of flags: it is an argument.
		"idea list -- --json":       false,
		"idea show -- --json extra": false,
		// A later --json=false overrides.
		"idea list --json --json=false extra": false,

		// The same three after the argument pflag refuses.
		"idea list --bogus --project --json":    false,
		"workitem link --bogus --role --json":   false,
		"idea list --bogus -- --json":           false,
		"idea list --bogus --json --json=false": false,
		"idea list --json=false --bogus --json": true,
		"idea list --json --bogus --json=false": false,
	} {
		t.Run(argv, func(t *testing.T) {
			if got := refusalForm(t, argv); got != wantEnvelope {
				t.Fatalf("camp %s: envelope = %v, want %v", argv, got, wantEnvelope)
			}
		})
	}
}
