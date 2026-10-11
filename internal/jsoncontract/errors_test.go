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

// TestRequested_HonorsExplicitFalse is the regression for PR #313 review:
// a caller writing `--json=false` (or =0/no/off) must be honored. The
// prior implementation matched any `--json=` argv as opt-in, so an
// explicit disable still rendered the JSON error envelope and turned a
// human-mode failure into a JSON one. Tests run inside the package so
// the os.Args fallback path is exercised directly.
func TestRequested_HonorsExplicitFalse(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want bool
	}{
		{"bare --json", []string{"camp", "--json"}, true},
		{"--json=true", []string{"camp", "--json=true"}, true},
		{"--json=1", []string{"camp", "--json=1"}, true},
		{"--json=yes", []string{"camp", "--json=yes"}, true},
		{"--json=t", []string{"camp", "--json=t"}, true},
		{"--json=false", []string{"camp", "--json=false"}, false},
		{"--json=0", []string{"camp", "--json=0"}, false},
		{"--json=False", []string{"camp", "--json=False"}, false},
		{"--json=FALSE", []string{"camp", "--json=FALSE"}, false},
		{"--json=f", []string{"camp", "--json=f"}, false},
		{"no json flag at all", []string{"camp", "workitem", "commit"}, false},
		{"--json=pretty (unparseable falls back to enabled)",
			[]string{"camp", "--json=pretty"}, true},
	}
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Args = tc.argv
			got := Requested(nil)
			if got != tc.want {
				t.Errorf("Requested(nil) with argv=%v = %v, want %v", tc.argv, got, tc.want)
			}
		})
	}
}

// TestRequested_CallbackWins covers the path where cobra has already
// parsed --json into the bound bool and the callback returns true. The
// argv scan is a fallback for flag-parse errors, not the primary signal.
func TestRequested_CallbackWins(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"camp", "workitem", "commit"}
	if !Requested(func() bool { return true }) {
		t.Error("callback returning true must be honored even when argv lacks --json")
	}
	if Requested(func() bool { return false }) {
		t.Error("callback returning false must short-circuit; argv has no --json so result must be false")
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
