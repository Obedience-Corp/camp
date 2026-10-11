package dungeon

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
)

// runExploreCommand runs `camp dungeon explore` through cobra, with the output
// settings the real root command has, and returns what main would see.
func runExploreCommand(t *testing.T, terminal bool, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	origTerminal := exploreTerminal
	exploreTerminal = func() bool { return terminal }
	rootErrors, rootUsage := Cmd.SilenceErrors, Cmd.SilenceUsage
	cmdErrors, cmdUsage := dungeonExploreCmd.SilenceErrors, dungeonExploreCmd.SilenceUsage
	Cmd.SilenceErrors, Cmd.SilenceUsage = true, true
	Cmd.SetOut(&out)
	Cmd.SetErr(&errOut)
	Cmd.SetArgs(append([]string{"explore"}, args...))
	defer func() {
		exploreTerminal = origTerminal
		Cmd.SilenceErrors, Cmd.SilenceUsage = rootErrors, rootUsage
		dungeonExploreCmd.SilenceErrors, dungeonExploreCmd.SilenceUsage = cmdErrors, cmdUsage
		Cmd.SetOut(nil)
		Cmd.SetErr(nil)
		Cmd.SetArgs(nil)
		dungeonExploreCmd.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}()
	err = Cmd.ExecuteContext(context.Background())
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
	cases := append(exploreRefusals[:len(exploreRefusals):len(exploreRefusals)], struct {
		name string
		args []string
		want string
	}{"unknown flag", []string{"--bogus"}, "unknown flag: --bogus"})
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
				if envelope.SchemaVersion != explore.SchemaVersion {
					t.Fatalf("schema_version = %q, want %q", envelope.SchemaVersion, explore.SchemaVersion)
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
