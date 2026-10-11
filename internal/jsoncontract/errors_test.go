package jsoncontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// probeCommand has the flag shapes the scan must tell apart: a boolean, a
// value flag with a shorthand, and a flag whose value is optional.
func probeCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return nil }}
	cmd.Flags().Bool("json", false, "")
	cmd.Flags().BoolP("all", "a", false, "")
	cmd.Flags().StringP("status", "s", "", "")
	cmd.Flags().String("campaign", "", "")
	cmd.Flags().Lookup("campaign").NoOptDefVal = "current"
	return cmd
}

// A flag pflag refuses stops parsing, and a --json after it is never read.
// The scan reads argv for it the way pflag would have.
//
// The explicit-false rows are the regression for PR #313 review: a caller
// writing --json=false (or =0, =f, any case) must be honored, where an
// earlier scan took any --json= for a request.
func TestScanJSONFlagReadsArgvTheWayPflagDoes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		argv  []string
		want  bool
		found bool
	}{
		{"bare --json", []string{"--json"}, true, true},
		{"--json=true", []string{"--json=true"}, true, true},
		{"--json=1", []string{"--json=1"}, true, true},
		{"--json=t", []string{"--json=t"}, true, true},
		{"--json=false", []string{"--json=false"}, false, true},
		{"--json=0", []string{"--json=0"}, false, true},
		{"--json=False", []string{"--json=False"}, false, true},
		{"--json=FALSE", []string{"--json=FALSE"}, false, true},
		{"--json=f", []string{"--json=f"}, false, true},
		{"--json=pretty is a request", []string{"--json=pretty"}, true, true},
		{"no json flag at all", []string{"workitem", "commit", "--all"}, false, false},
		{"among subcommand names and positionals", []string{"dungeon", "explore", "--json", "extra"}, true, true},

		{"the value of a long flag", []string{"--status", "--json"}, false, false},
		{"the value of a shorthand", []string{"-s", "--json"}, false, false},
		{"the value of the last shorthand in a group", []string{"-as", "--json"}, false, false},
		{"after a long flag with its value attached", []string{"--status=done", "--json"}, true, true},
		{"after a shorthand with its value attached", []string{"-sdone", "--json"}, true, true},
		{"after a shorthand with = and its value", []string{"-s=done", "--json"}, true, true},
		{"after a long flag and its value", []string{"--status", "done", "--json"}, true, true},
		{"after a boolean", []string{"--all", "--json"}, true, true},
		{"after a boolean shorthand", []string{"-a", "--json"}, true, true},
		{"after a flag whose value is optional", []string{"--campaign", "--json"}, true, true},
		{"after a flag the command does not define", []string{"--bogus", "--json"}, true, true},
		{"after a shorthand the command does not define", []string{"-x", "--json"}, true, true},

		{"after the end of flags", []string{"--", "--json"}, false, false},
		{"before the end of flags", []string{"--json", "--", "--json=false"}, true, true},
		{"a later false wins", []string{"--json", "--json=false"}, false, true},
		{"a later true wins", []string{"--json=false", "--json"}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, found := scanJSONFlag(probeCommand().Flags(), tc.argv)
			if got != tc.want || found != tc.found {
				t.Errorf("scanJSONFlag(%q) = %v, found %v; want %v, found %v", tc.argv, got, found, tc.want, tc.found)
			}
		})
	}
}

// Once cobra has parsed the flags, the command's own answer decides. argv is
// not read again: it cannot tell the --json flag from a --json that is a
// value or follows --.
func TestRequestedIsTheParsedAnswer(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"camp", "probe", "--status", "--json"}

	cmd := probeCommand()
	if !Requested(cmd, func() bool { return true }) {
		t.Error("the command's answer is true and was not honored")
	}
	if Requested(cmd, func() bool { return false }) {
		t.Error("argv holds a --json that is a flag's value, and it was taken for the flag")
	}

	// Without an answer of its own a command is asked through its --json flag.
	if Requested(cmd, nil) {
		t.Error("--json is not set and the command was taken to want JSON")
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	if !Requested(cmd, nil) {
		t.Error("--json is set and the command was not taken to want JSON")
	}
}

// A refusal renders in one form, picked by the flags as cobra reads them:
// before parsing stops, from what was parsed; after, from the rest of argv.
func TestFlagErrorFunc_FollowsTheFlagsNotTheFirstJSONToken(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		wantJSON bool
	}{
		{"json before the refused flag", []string{"--json", "--bogus"}, true},
		{"json after the refused flag", []string{"--bogus", "--json"}, true},
		{"json=false after the refused flag", []string{"--bogus", "--json=false"}, false},
		{"json then a later false", []string{"--json", "--bogus", "--json=false"}, false},
		{"false then a later json", []string{"--json=false", "--bogus", "--json"}, true},
		{"json is a flag's value", []string{"--bogus", "--status", "--json"}, false},
		{"json follows the end of flags", []string{"--bogus", "--", "--json"}, false},
		{"no json", []string{"--bogus"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			os.Args = append([]string{"probe"}, tc.args...)
			defer func() { os.Args = origArgs }()

			cmd := probeCommand()
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			asJSON := func() bool { v, _ := cmd.Flags().GetBool("json"); return v }
			cmd.SetFlagErrorFunc(FlagErrorFunc("probe/v1", asJSON))
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)
			cmd.SetOut(io.Discard)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			var cmdErr *camperrors.CommandError
			if !errors.As(err, &cmdErr) || cmdErr.ExitCode != 2 {
				t.Fatalf("Execute() = %v, want a command error with exit code 2", err)
			}
			var envelope ErrorEnvelope
			gotJSON := json.Unmarshal(stderr.Bytes(), &envelope) == nil
			if gotJSON != tc.wantJSON {
				t.Fatalf("envelope rendered = %v, want %v; stderr %q", gotJSON, tc.wantJSON, stderr.String())
			}
		})
	}
}

// Every way pflag refuses an argument is a usage error: exit 2 and a
// validation_error envelope, whichever command renders it.
func TestFlagErrorFunc_ParseFailuresAreUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing value", []string{"--status"}, "flag needs an argument: --status"},
		{"missing shorthand value", []string{"-s"}, "flag needs an argument: 's' in -s"},
		{"unknown shorthand", []string{"-x"}, "unknown shorthand flag: 'x' in -x"},
		{"unknown flag", []string{"--bogus"}, "unknown flag: --bogus"},
		{"bad syntax", []string{"--=x"}, "bad flag syntax: --=x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return nil }}
			cmd.Flags().StringP("status", "s", "", "")
			cmd.SetFlagErrorFunc(FlagErrorFunc("probe/v1", func() bool { return true }))
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)
			cmd.SetOut(io.Discard)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			var cmdErr *camperrors.CommandError
			if !errors.As(err, &cmdErr) || cmdErr.ExitCode != 2 {
				t.Fatalf("Execute() = %v, want a command error with exit code 2", err)
			}
			var envelope ErrorEnvelope
			if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
				t.Fatalf("stderr is not one JSON envelope: %q (%v)", stderr.String(), err)
			}
			if envelope.Error.Code != "validation_error" || envelope.Error.ExitCode != 2 || envelope.Error.Message != tc.want {
				t.Fatalf("error = %+v, want a validation_error with exit_code 2 and message %q", envelope.Error, tc.want)
			}
		})
	}
}
