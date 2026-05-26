package jsoncontract

import (
	"os"
	"testing"
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