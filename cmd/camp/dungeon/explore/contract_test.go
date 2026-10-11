package explore

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
)

var exploreParent = func() *cobra.Command {
	parent := &cobra.Command{Use: "dungeon"}
	parent.AddCommand(Cmd)
	return parent
}()

// runExploreCommand runs `camp dungeon explore` through cobra, with the output
// settings the real root command has, and returns what main would see.
func runExploreCommand(t *testing.T, terminal bool, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	origTerminal := exploreTerminal
	exploreTerminal = func() bool { return terminal }
	rootErrors, rootUsage := exploreParent.SilenceErrors, exploreParent.SilenceUsage
	cmdErrors, cmdUsage := Cmd.SilenceErrors, Cmd.SilenceUsage
	exploreParent.SilenceErrors, exploreParent.SilenceUsage = true, true
	exploreParent.SetOut(&out)
	exploreParent.SetErr(&errOut)
	exploreParent.SetArgs(append([]string{"explore"}, args...))
	defer func() {
		exploreTerminal = origTerminal
		exploreParent.SilenceErrors, exploreParent.SilenceUsage = rootErrors, rootUsage
		Cmd.SilenceErrors, Cmd.SilenceUsage = cmdErrors, cmdUsage
		exploreParent.SetOut(nil)
		exploreParent.SetErr(nil)
		exploreParent.SetArgs(nil)
		Cmd.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}()
	err = exploreParent.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

// exitCode is the code main exits with: it reads a CommandError returned
// directly and treats anything else as 1.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if cmdErr, ok := err.(*camperrors.CommandError); ok && cmdErr.ExitCode != 0 {
		return cmdErr.ExitCode
	}
	return 1
}

var exploreRefusals = []struct {
	name string
	args []string
	want string
}{
	{"since is not a date", []string{"--since", "2026-13-01"}, `--since must be YYYY-MM-DD, got "2026-13-01"`},
	{"until is not a date", []string{"--until", "soon"}, `--until must be YYYY-MM-DD, got "soon"`},
	{"since after until", []string{"--since", "2026-10-02", "--until", "2026-10-01"}, "--since is after --until"},
	{"unknown images", []string{"--images", "bogus"}, `unknown --images "bogus"; use auto, kitty, iterm, or off`},
	{"unknown images with no media", []string{"--no-media", "--images", "bogus"}, `unknown --images "bogus"; use auto, kitty, iterm, or off`},
	{"extra argument", []string{"extra"}, `unknown command "extra" for "dungeon explore"`},
}

func TestExploreRefusalsExitTwoWithJSONEnvelope(t *testing.T) {
	modes := []struct {
		name     string
		terminal bool
		flags    []string
	}{
		{"json flag", true, []string{"--json"}},
		{"pipe", false, nil},
	}
	type refusal = struct {
		name string
		args []string
		want string
	}
	// What pflag refuses never reaches the command, so these have no text
	// form to compare on a terminal: cobra prints them with the usage.
	cases := append(exploreRefusals[:len(exploreRefusals):len(exploreRefusals)],
		refusal{"unknown flag", []string{"--bogus"}, "unknown flag: --bogus"},
		refusal{"flag without its value", []string{"--status"}, "flag needs an argument: --status"},
		refusal{"unknown shorthand", []string{"-x"}, "unknown shorthand flag: 'x' in -x"},
	)
	for _, mode := range modes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				stdout, stderr, err := runExploreCommand(t, mode.terminal, append(mode.flags, tc.args...)...)
				if got := exitCode(err); got != 2 {
					t.Fatalf("exit code = %d (%v), want 2", got, err)
				}
				if stdout != "" {
					t.Fatalf("stdout = %q, want nothing", stdout)
				}
				var envelope jsoncontract.ErrorEnvelope
				if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
					t.Fatalf("stderr is not one JSON envelope: %q (%v)", stderr, err)
				}
				if envelope.SchemaVersion != intexplore.SchemaVersion {
					t.Fatalf("schema_version = %q, want %q", envelope.SchemaVersion, intexplore.SchemaVersion)
				}
				if envelope.Error.Code != "validation_error" || envelope.Error.ExitCode != 2 || envelope.Error.Message != tc.want {
					t.Fatalf("error = %+v, want a validation_error with exit_code 2 and message %q", envelope.Error, tc.want)
				}
			})
		}
	}
}

func TestExploreRefusalsExitTwoOnATerminal(t *testing.T) {
	for _, tc := range exploreRefusals {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := runExploreCommand(t, true, tc.args...)
			if got := exitCode(err); got != 2 {
				t.Fatalf("exit code = %d (%v), want 2", got, err)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want nothing", stdout)
			}
			if want := "Error: " + tc.want + "\n"; stderr != want {
				t.Fatalf("stderr = %q, want %q", stderr, want)
			}
		})
	}
	// --json=false on a terminal is still the text path.
	_, stderr, err := runExploreCommand(t, true, "--json=false", "--since", "2026-13-01")
	if exitCode(err) != 2 || !strings.HasPrefix(stderr, "Error: --since must be YYYY-MM-DD") {
		t.Fatalf("--json=false: exit %d, stderr %q", exitCode(err), stderr)
	}
}

// A --json in argv is not always the flag. It can be another flag's value,
// follow --, or be overridden by a later --json=false, and cobra opens the
// feed for each. A refusal then comes out once, in the form the parsed flags
// picked: the text line on a terminal, the envelope on a pipe, never both
// and never neither.
func TestExploreRefusalIsReportedOnceWhateverArgvHolds(t *testing.T) {
	const badSince = `--since must be YYYY-MM-DD, got "2026-13-01"`
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"the value of another flag", []string{"--query", "--json", "--since", "2026-13-01"}, badSince},
		{"after the end of flags", []string{"--", "--json"}, `unknown command "--json" for "dungeon explore"`},
		{"overridden by a later false", []string{"--json", "--json=false", "--since", "2026-13-01"}, badSince},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// jsoncontract looks for --json in the process arguments.
			origArgs := os.Args
			os.Args = append([]string{"camp", "dungeon", "explore"}, tc.args...)
			defer func() { os.Args = origArgs }()

			stdout, stderr, err := runExploreCommand(t, true, tc.args...)
			if got := exitCode(err); got != 2 || stdout != "" {
				t.Fatalf("terminal: exit %d (%v), stdout %q; want 2 and nothing", got, err, stdout)
			}
			if want := "Error: " + tc.want + "\n"; stderr != want {
				t.Fatalf("terminal: stderr = %q, want only %q", stderr, want)
			}

			stdout, stderr, err = runExploreCommand(t, false, tc.args...)
			if got := exitCode(err); got != 2 || stdout != "" {
				t.Fatalf("pipe: exit %d (%v), stdout %q; want 2 and nothing", got, err, stdout)
			}
			var envelope jsoncontract.ErrorEnvelope
			if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
				t.Fatalf("pipe: stderr is not one JSON envelope: %q (%v)", stderr, err)
			}
			if envelope.Error.Message != tc.want || envelope.Error.ExitCode != 2 {
				t.Fatalf("pipe: error = %+v, want %q with exit_code 2", envelope.Error, tc.want)
			}
		})
	}
}
