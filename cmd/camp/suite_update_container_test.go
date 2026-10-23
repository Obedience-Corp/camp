//go:build container_fs

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Obedience-Corp/obey-shared/updatecheck"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/version"
)

// suiteFSFixture is a container HOME with the real checker wired in, both
// streams reporting a terminal, and a release server that counts requests so
// "no network" is asserted rather than assumed.
// suiteFSQuietEnv is every variable that would suppress the notice, cleared
// so each case controls exactly one.
var suiteFSQuietEnv = []string{
	"CI", "GITHUB_ACTIONS", updatecheck.EnvNoUpdateCheck,
	"OBEY_AGENT", "OBEY_SESSION_ID", "CLAUDE_CODE", "CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CODEX_TASK",
}

type suiteFSFixture struct {
	home     string
	cache    string
	requests *atomic.Int64
}

func newSuiteFSFixture(t *testing.T, bundle, latest string) suiteFSFixture {
	t.Helper()
	withSuiteColorProfile(t, termenv.Ascii)
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range append(suiteFSQuietEnv, suiteAgentEnv...) {
		t.Setenv(key, "")
	}

	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"tag_name":"v` + latest + `","html_url":"https://example.test/v` + latest + `"}`))
	}))
	t.Cleanup(srv.Close)

	prevBundle, prevVersion := version.Bundle, version.Version
	prevOut, prevErr, prevFetcher, prevHelp := stdoutIsTTY, stderrIsTTY, suiteFetcher, suiteHelpRendered
	t.Cleanup(func() {
		version.Bundle, version.Version = prevBundle, prevVersion
		stdoutIsTTY, stderrIsTTY, suiteFetcher, suiteHelpRendered = prevOut, prevErr, prevFetcher, prevHelp
	})
	version.Bundle, version.Version = bundle, "0.16.3"
	stdoutIsTTY = func() bool { return true }
	stderrIsTTY = func() bool { return true }
	suiteFetcher = &updatecheck.Fetcher{URL: srv.URL}
	suiteHelpRendered = false

	return suiteFSFixture{
		home:     home,
		cache:    filepath.Join(home, ".obey", "update-check", "suite.json"),
		requests: &requests,
	}
}

func (f suiteFSFixture) seed(t *testing.T, latest string, checkedAt time.Time) []byte {
	t.Helper()
	data, err := json.MarshalIndent(updatecheck.Cache{
		SchemaVersion: updatecheck.SchemaVersion,
		Source:        updatecheck.Source,
		Channel:       updatecheck.ChannelStable,
		CheckedAt:     checkedAt.UTC(),
		LatestTag:     "v" + latest,
		LatestVersion: latest,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(f.cache), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.cache, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

func suiteFSCommand(t *testing.T, setup func(*cobra.Command), args ...string) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "camp"}
	leaf := &cobra.Command{Use: "status"}
	if setup != nil {
		setup(leaf)
	}
	root.AddCommand(leaf)
	if err := leaf.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	leaf.SetContext(context.Background())
	return leaf
}

func TestSuiteUpdateFSNoticeOncePerRelease(t *testing.T) {
	f := newSuiteFSFixture(t, "v0.3.20", "0.3.24")
	f.seed(t, "0.3.24", time.Now())

	var first bytes.Buffer
	maybeNotifySuiteUpdate(suiteFSCommand(t, nil), &first)

	lines := strings.Split(strings.TrimSuffix(first.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("notice is %d lines, want 2: %q", len(lines), first.String())
	}
	if lines[0] != "⚠ notice: Festival suite update available (0.3.20 → 0.3.24)" {
		t.Fatalf("message line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  run: ") && !strings.HasPrefix(lines[1], "  see: ") {
		t.Fatalf("action line = %q", lines[1])
	}

	var stored updatecheck.Cache
	data, err := os.ReadFile(f.cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.LastNotifiedVersion != "0.3.24" || stored.LastNotifiedAt.IsZero() {
		t.Fatalf("notice was not recorded: %s", data)
	}

	var second bytes.Buffer
	maybeNotifySuiteUpdate(suiteFSCommand(t, nil), &second)
	if second.Len() != 0 {
		t.Fatalf("second run repeated the notice: %q", second.String())
	}
	if n := f.requests.Load(); n != 0 {
		t.Fatalf("a fresh cache made %d release requests", n)
	}
}

// Every suppressed path is checked against a stale cache: if suppression
// leaked, the checker would refresh from the counting server.
func TestSuiteUpdateFSSuppressedPathsDoNoWork(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T)
		cmd   func(t *testing.T) *cobra.Command
	}{
		{"CI", func(t *testing.T) { t.Setenv("CI", "true") }, nil},
		{"GITHUB_ACTIONS", func(t *testing.T) { t.Setenv("GITHUB_ACTIONS", "true") }, nil},
		{"opt-out", func(t *testing.T) { t.Setenv(updatecheck.EnvNoUpdateCheck, "1") }, nil},
		{"agent", func(t *testing.T) { t.Setenv("CLAUDECODE", "1") }, nil},
		{"camp agent session", func(t *testing.T) { t.Setenv("OBEY_SESSION", "s-1") }, nil},
		{"stderr not a terminal", func(*testing.T) { stderrIsTTY = func() bool { return false } }, nil},
		{"stdout captured", func(*testing.T) { stdoutIsTTY = func() bool { return false } }, nil},
		{"--json", nil, func(t *testing.T) *cobra.Command {
			return suiteFSCommand(t, func(c *cobra.Command) { c.Flags().Bool("json", false, "") }, "--json")
		}},
		{"--print", nil, func(t *testing.T) *cobra.Command {
			return suiteFSCommand(t, func(c *cobra.Command) { c.Flags().Bool("print", false, "") }, "--print")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSuiteFSFixture(t, "v0.3.20", "0.3.24")
			before := f.seed(t, "0.3.22", time.Now().Add(-48*time.Hour))
			if tc.setup != nil {
				tc.setup(t)
			}
			cmd := suiteFSCommand(t, nil)
			if tc.cmd != nil {
				cmd = tc.cmd(t)
			}

			var out bytes.Buffer
			maybeNotifySuiteUpdate(cmd, &out)

			if out.Len() != 0 {
				t.Fatalf("suppressed path printed %q", out.String())
			}
			if n := f.requests.Load(); n != 0 {
				t.Fatalf("suppressed path made %d release requests", n)
			}
			after, err := os.ReadFile(f.cache)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("suppressed path rewrote the cache:\n%s", after)
			}
		})
	}
}

func TestSuiteUpdateFSEmptyBundleCreatesNoCache(t *testing.T) {
	f := newSuiteFSFixture(t, "", "0.3.24")

	var out bytes.Buffer
	maybeNotifySuiteUpdate(suiteFSCommand(t, nil), &out)
	info, err := suiteVersionCheck(context.Background(), "0.16.3", "")
	if err != nil {
		t.Fatal(err)
	}

	if out.Len() != 0 {
		t.Fatalf("empty bundle printed %q", out.String())
	}
	if info.SkipReason != suiteSkipNotRelease || info.LatestVersion != "" {
		t.Fatalf("version --check on an empty bundle = %+v", info)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".obey")); !os.IsNotExist(err) {
		t.Fatalf("empty bundle created ~/.obey (stat err %v)", err)
	}
	if n := f.requests.Load(); n != 0 {
		t.Fatalf("empty bundle made %d release requests", n)
	}
}

func TestSuiteUpdateFSStaleCacheRefreshes(t *testing.T) {
	f := newSuiteFSFixture(t, "v0.3.20", "0.3.24")
	f.seed(t, "0.3.22", time.Now().Add(-48*time.Hour))

	var out bytes.Buffer
	maybeNotifySuiteUpdate(suiteFSCommand(t, nil), &out)

	if n := f.requests.Load(); n != 1 {
		t.Fatalf("stale cache made %d release requests, want 1", n)
	}
	if !strings.Contains(out.String(), "(0.3.20 → 0.3.24)") {
		t.Fatalf("refreshed notice = %q", out.String())
	}
}

func TestSuiteUpdateFSUnresponsiveServerStaysWithinBudget(t *testing.T) {
	f := newSuiteFSFixture(t, "v0.3.20", "0.3.24")
	f.seed(t, "0.3.22", time.Now().Add(-48*time.Hour))
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); srv.Close() })
	suiteFetcher = &updatecheck.Fetcher{URL: srv.URL}

	start := time.Now()
	var out bytes.Buffer
	maybeNotifySuiteUpdate(suiteFSCommand(t, nil), &out)
	elapsed := time.Since(start)

	if elapsed > updatecheck.AmbientTimeout+750*time.Millisecond {
		t.Fatalf("ambient check took %s against a hung server, budget %s", elapsed, updatecheck.AmbientTimeout)
	}
	if !strings.Contains(out.String(), "(0.3.20 → 0.3.22)") {
		t.Fatalf("a failed refresh should fall back to the cached release, got %q", out.String())
	}
}

func TestSuiteUpdateFSVersionCheck(t *testing.T) {
	t.Run("behind", func(t *testing.T) {
		f := newSuiteFSFixture(t, "v0.3.20", "0.3.24")
		f.seed(t, "0.3.24", time.Now())

		info, err := suiteVersionCheck(context.Background(), "0.16.3", "v0.3.20")
		if err != nil {
			t.Fatal(err)
		}
		if info.CurrentVersion != "0.3.20" || info.CampVersion != "0.16.3" || info.LatestVersion != "0.3.24" || !info.UpdateAvailable {
			t.Fatalf("check = %+v", info)
		}
		if info.UpgradeCommand == "" || info.CheckedAt == "" {
			t.Fatalf("check is missing the upgrade command or checked_at: %+v", info)
		}
		var human bytes.Buffer
		if err := writeVersionCheck(&human, info); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"camp 0.16.3 (festival suite 0.3.20)\n", "festival suite latest: 0.3.24\n", "update available\n"} {
			if !strings.Contains(human.String(), want) {
				t.Errorf("human output missing %q:\n%s", want, human.String())
			}
		}
		if n := f.requests.Load(); n != 0 {
			t.Fatalf("fresh cache made %d release requests", n)
		}
	})
	t.Run("stale cache and GitHub unreachable", func(t *testing.T) {
		f := newSuiteFSFixture(t, "v0.3.20", "0.3.24")
		f.seed(t, "0.3.20", time.Now().Add(-72*time.Hour))
		closed := httptest.NewServer(http.NotFoundHandler())
		closed.Close()
		suiteFetcher = &updatecheck.Fetcher{URL: closed.URL}

		info, err := suiteVersionCheck(context.Background(), "0.16.3", "v0.3.20")
		if err != nil {
			t.Fatal(err)
		}
		if !info.FromCache || info.RefreshError == "" {
			t.Fatalf("a failed refresh was not reported: %+v", info)
		}
		var human bytes.Buffer
		if err := writeVersionCheck(&human, info); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(human.String(), "festival suite latest: 0.3.20 (cached ") ||
			!strings.Contains(human.String(), "could not reach GitHub to refresh)") {
			t.Fatalf("human output hides the failed refresh:\n%s", human.String())
		}
	})
	t.Run("current", func(t *testing.T) {
		f := newSuiteFSFixture(t, "v0.3.24", "0.3.24")
		f.seed(t, "0.3.24", time.Now())

		info, err := suiteVersionCheck(context.Background(), "0.16.3", "v0.3.24")
		if err != nil {
			t.Fatal(err)
		}
		if info.UpdateAvailable || info.UpgradeCommand != "" {
			t.Fatalf("an up-to-date suite reported an update: %+v", info)
		}
		var human bytes.Buffer
		if err := writeVersionCheck(&human, info); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(human.String(), "up to date\n") {
			t.Fatalf("human output:\n%s", human.String())
		}
	})
}
