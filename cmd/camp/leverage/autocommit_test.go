package leverage

import (
	"strings"
	"testing"

	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

func newAutocommitTestCmd(t *testing.T, noCommit bool) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "leverage"}
	addNoCommitFlag(cmd)
	if noCommit {
		if err := cmd.Flags().Set(noCommitFlag, "true"); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

func TestAutocommitDisabled(t *testing.T) {
	off, on := false, true

	tests := []struct {
		name       string
		noCommit   bool
		input      autocommitInput
		wantOff    bool
		wantReason string
	}{
		{
			name:       "--no-commit wins over an enabling config",
			noCommit:   true,
			input:      autocommitInput{cfg: &intleverage.LeverageConfig{Autocommit: &on}},
			wantOff:    true,
			wantReason: "--no-commit",
		},
		{
			name:       "--no-commit wins over ignoreConfigOptOut",
			noCommit:   true,
			input:      autocommitInput{cfg: &intleverage.LeverageConfig{}, ignoreConfigOptOut: true},
			wantOff:    true,
			wantReason: "--no-commit",
		},
		{
			name:       "config opt-out disables",
			input:      autocommitInput{cfg: &intleverage.LeverageConfig{Autocommit: &off}},
			wantOff:    true,
			wantReason: "autocommit is disabled in",
		},
		{
			name:    "config opt-out is overridden when recording the opt-out itself",
			input:   autocommitInput{cfg: &intleverage.LeverageConfig{Autocommit: &off}, ignoreConfigOptOut: true},
			wantOff: false,
		},
		{
			name:    "default config allows the commit",
			input:   autocommitInput{cfg: &intleverage.LeverageConfig{}},
			wantOff: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason, off := autocommitDisabled(newAutocommitTestCmd(t, tc.noCommit), tc.input)
			if off != tc.wantOff {
				t.Fatalf("disabled = %t, want %t (reason %q)", off, tc.wantOff, reason)
			}
			if !tc.wantOff {
				return
			}
			if !strings.Contains(reason, tc.wantReason) {
				t.Errorf("reason = %q, want it to contain %q", reason, tc.wantReason)
			}
		})
	}
}

func TestShortSHA(t *testing.T) {
	tests := []struct {
		name string
		sha  string
		want string
	}{
		{name: "empty stays empty", sha: "", want: ""},
		{name: "short sha is returned whole", sha: "abc12", want: "abc12"},
		{name: "full sha is truncated to seven", sha: "1ba882a5f0e1d2c3", want: "1ba882a"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortSHA(tc.sha); got != tc.want {
				t.Errorf("shortSHA(%q) = %q, want %q", tc.sha, got, tc.want)
			}
		})
	}
}
