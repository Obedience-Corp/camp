package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/version"
)

// seedVersionCache writes a probe result with an explicit age, so the freshness
// rule is tested rather than raced against wall-clock.
func seedVersionCache(t *testing.T, id, ver, commit string, age time.Duration) {
	t.Helper()
	dir := mustMachineCacheDir(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(versionCacheEntry{
		Version:  ver,
		Commit:   commit,
		ProbedAt: time.Now().Add(-age).UnixNano(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mustVersionCachePath(t, id), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHopSkewWarningMatrix(t *testing.T) {
	local := version.Get()

	tests := []struct {
		name    string
		seed    bool
		ver     string
		commit  string
		age     time.Duration
		wantHit bool
	}{
		{name: "cache miss says nothing", seed: false},
		{
			name: "same version and commit is silent",
			seed: true, ver: local.Version, commit: local.Commit, age: time.Minute,
		},
		{
			name: "different version warns",
			seed: true, ver: "v0.0.1-ancient", commit: "deadbeef", age: time.Minute,
			wantHit: true,
		},
		{
			// Two "dev" builds from different commits are not the same camp.
			name: "same version, different commit warns",
			seed: true, ver: local.Version, commit: "0000000000", age: time.Minute,
			wantHit: true,
		},
		{
			name: "stale entry is ignored, even when it shows skew",
			seed: true, ver: "v0.0.1-ancient", commit: "deadbeef", age: versionCacheTTL + time.Hour,
		},
		{
			// A failed probe records nothing, so an empty version must never be
			// read as "the remote has no version" and warned about.
			name: "empty version is not skew",
			seed: true, ver: "", commit: "", age: time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if tt.seed {
				seedVersionCache(t, "archdtop", tt.ver, tt.commit, tt.age)
			}
			got := hopSkewWarning("archdtop")
			if (got != "") != tt.wantHit {
				t.Fatalf("hopSkewWarning = %q, want present=%v", got, tt.wantHit)
			}
			if tt.wantHit {
				for _, want := range []string{"archdtop", tt.ver, local.Version, "camp machine diagnose archdtop"} {
					if want != "" && !strings.Contains(got, want) {
						t.Errorf("warning %q missing %q", got, want)
					}
				}
				// When versions match, the commit is the only disambiguator —
				// the warning must surface it rather than "dev" vs "dev".
				if tt.ver == local.Version && tt.commit != "" && !strings.Contains(got, tt.commit) {
					t.Errorf("warning %q missing commit disambiguation %q", got, tt.commit)
				}
			}
		})
	}
}

func TestVersionCacheWriteSkipsEmptyProbe(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeMachineVersionCache("archdtop", "", "")
	if _, err := os.Stat(mustVersionCachePath(t, "archdtop")); !os.IsNotExist(err) {
		t.Error("a failed probe must not be cached; it would look like a real answer")
	}
}

func TestVersionCacheCorruptIsIgnored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(mustMachineCacheDir(t), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mustVersionCachePath(t, "archdtop"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readMachineVersionCache("archdtop"); ok {
		t.Error("corrupt cache must read as a miss")
	}
	if hopSkewWarning("archdtop") != "" {
		t.Error("corrupt cache must not produce a warning")
	}
}

func TestVersionCacheEmptyVersionIsMiss(t *testing.T) {
	// Defense in depth: an empty version field must not be treated as a probe
	// answer even if the file is otherwise well-formed and fresh.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	seedVersionCache(t, "archdtop", "", "deadbeef", time.Minute)
	if _, ok := readMachineVersionCache("archdtop"); ok {
		t.Error("empty version must read as a miss")
	}
	if hopSkewWarning("archdtop") != "" {
		t.Error("empty version must not produce a warning")
	}
}

func TestVersionCacheLivesBesideTheCompletionCache(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got, want := filepath.Dir(mustVersionCachePath(t, "x")), mustMachineCacheDir(t); got != want {
		t.Errorf("version cache dir = %q, want %q", got, want)
	}
}

func TestSkewWarningGoesToStderrAndKeepsStdoutToOneLine(t *testing.T) {
	// The wrapper evals stdout. A warning that landed there would be eval'd as
	// a shell command.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// Hop emission resolves a dial endpoint; the escape hatch keeps this test
	// off the live resolver and the tailscale CLI.
	t.Setenv(remote.NoPeerFallbackEnv, "1")
	seedVersionCache(t, "archdtop", "v0.0.1-ancient", "deadbeef", time.Minute)

	var out, errb bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&out)
	cmd.SetErr(&errb)

	m := &machines.Machine{ID: "archdtop", Host: "archdtop.ts.net", SSHUser: "lance"}
	if err := emitHopOrRefuse(context.Background(), cmd, m, "campaign", "/srv/campaign", true); err != nil {
		t.Fatal(err)
	}

	if strings.Count(out.String(), "\n") != 1 {
		t.Errorf("stdout must stay exactly one line, got %q", out.String())
	}
	if strings.Contains(out.String(), "diagnose") {
		t.Errorf("warning leaked into the eval'd line: %q", out.String())
	}
	if !strings.Contains(errb.String(), "camp on archdtop is v0.0.1-ancient") {
		t.Errorf("stderr = %q, want the skew warning", errb.String())
	}
}
