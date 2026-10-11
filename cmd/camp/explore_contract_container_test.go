//go:build container_fs

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
)

// executeExplore runs `camp dungeon explore` through Execute, as main does,
// and returns the exit code main would use.
func executeExplore(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(nil)
	oldArgs := os.Args
	os.Args = append([]string{"camp", "dungeon", "explore"}, args...)
	defer func() {
		os.Args = oldArgs
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		cmd, _, err := rootCmd.Find([]string{"dungeon", "explore"})
		if err != nil {
			t.Fatal(err)
		}
		cmd.SilenceErrors, cmd.SilenceUsage = false, false
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}()
	err := Execute(context.Background())
	if err == nil {
		return out.String(), errOut.String(), 0
	}
	if code, silent := silentExit(err); silent {
		return out.String(), errOut.String(), code
	}
	return out.String(), errOut.String(), 1
}

func TestExploreExecuteRefusesInvalidFlags(t *testing.T) {
	t.Setenv("CAMP_ROOT", "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"), []byte("id: fixture\nname: fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := filepath.Join(root, "festivals", ".dungeon", "completed")
	if err := os.MkdirAll(done, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(done, "note.md"), []byte("# Note\n\nShipped.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chdirForTest(t, root)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"since", []string{"--since", "2026-13-01"}, `--since must be YYYY-MM-DD, got "2026-13-01"`},
		{"since after until", []string{"--since", "2026-10-02", "--until", "2026-10-01"}, "--since is after --until"},
		{"status", []string{"--status", "bogus"}, `unknown status "bogus"; use finished, completed, done, archived, someday, killed, holding, all, or a status directory name`},
		{"dungeon", []string{"--dungeon", "nope"}, `unknown dungeon "nope"; use all, a dungeon label, or a camp-relative dungeon path`},
		{"images", []string{"--images", "bogus"}, `unknown --images "bogus"; use auto, kitty, iterm, or off`},
		{"unknown flag", []string{"--bogus"}, "unknown flag: --bogus"},
		{"flag without its value", []string{"--status"}, "flag needs an argument: --status"},
		{"unknown shorthand", []string{"-x"}, "unknown shorthand flag: 'x' in -x"},
		{"dungeon below a dungeon", []string{"--dungeon", "festivals/.dungeon/completed/"}, `unknown dungeon "festivals/.dungeon/completed/"; use all, a dungeon label, or a camp-relative dungeon path`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := executeExplore(t, append([]string{"--json"}, tc.args...)...)
			if code != 2 {
				t.Fatalf("exit code = %d, want 2\nstderr: %s", code, stderr)
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

	// The dungeon is spelled the way a shell completes its path.
	stdout, stderr, code := executeExplore(t, "--json", "--status", "completed", "--dungeon", "./festivals/.dungeon/")
	if code != 0 {
		t.Fatalf("valid flags exited %d: %s", code, stderr)
	}
	var payload struct {
		SchemaVersion string `json:"schema_version"`
		CampaignRoot  string `json:"campaign_root"`
		Items         []struct {
			Path string `json:"path"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %q (%v)", stdout, err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != explore.SchemaVersion || payload.CampaignRoot != resolved {
		t.Fatalf("schema_version = %q, campaign_root = %q; want %q and %q", payload.SchemaVersion, payload.CampaignRoot, explore.SchemaVersion, resolved)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items = %+v, want the one completed note", payload.Items)
	}
	if _, err := os.Stat(filepath.Join(payload.CampaignRoot, filepath.FromSlash(payload.Items[0].Path))); err != nil {
		t.Fatalf("campaign_root joined with the item path does not resolve: %v", err)
	}
}
