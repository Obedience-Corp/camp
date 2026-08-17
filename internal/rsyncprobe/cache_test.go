package rsyncprobe

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// The cache is derived state: a JSON file, no git, no workflow mutation, so it
// follows the same host-t.TempDir pattern the config package uses rather than
// the container lane, which exists for filesystem workflows.

// fakeClock returns a controllable time source.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// countingProbe records how many times the real probe would have run.
type countingProbe struct {
	calls  int
	engine Engine
	err    error
}

func (c *countingProbe) run() (Engine, error) {
	c.calls++
	return c.engine, c.err
}

func newTestProber(t *testing.T, clock *fakeClock, noCache bool) (*Prober, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rsync-probe.json")
	return newProberAt(path, clock.now, noCache), path
}

func TestCacheHitWithinTTL(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)}
	p, _ := newTestProber(t, clock, false)
	probe := &countingProbe{engine: Engine{Kind: KindRsync, Version: "3.4.4", Protocol: 32}}

	first, err := p.cached(context.Background(), "studio", probe.run)
	if err != nil {
		t.Fatalf("first probe error = %v", err)
	}
	if probe.calls != 1 {
		t.Fatalf("probe ran %d times on a cold cache, want 1", probe.calls)
	}

	clock.advance(23 * time.Hour)
	second, err := p.cached(context.Background(), "studio", probe.run)
	if err != nil {
		t.Fatalf("second probe error = %v", err)
	}
	if probe.calls != 1 {
		t.Errorf("probe ran %d times within the TTL, want it served from cache", probe.calls)
	}
	if second != first {
		t.Errorf("cached engine = %+v, want %+v", second, first)
	}
}

func TestCacheExpiresAfterTTL(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)}
	p, _ := newTestProber(t, clock, false)
	probe := &countingProbe{engine: Engine{Kind: KindRsync, Protocol: 32}}

	if _, err := p.cached(context.Background(), "studio", probe.run); err != nil {
		t.Fatalf("first probe error = %v", err)
	}

	// Exactly at the TTL the entry is already stale: freshness is a strict
	// less-than, so a 24h-old answer is re-probed rather than trusted.
	clock.advance(cacheTTL)
	if _, err := p.cached(context.Background(), "studio", probe.run); err != nil {
		t.Fatalf("second probe error = %v", err)
	}
	if probe.calls != 2 {
		t.Errorf("probe ran %d times, want 2 (entry expired at the TTL boundary)", probe.calls)
	}
}

func TestNoProbeCacheBypassesAndRefreshes(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "rsync-probe.json")

	// Seed a fresh entry through a normal prober.
	warm := newProberAt(path, clock.now, false)
	stale := &countingProbe{engine: Engine{Kind: KindOpenRsync, Protocol: 29}}
	if _, err := warm.cached(context.Background(), "studio", stale.run); err != nil {
		t.Fatalf("seed error = %v", err)
	}

	// --no-probe-cache must re-probe even though the entry is fresh.
	bypass := newProberAt(path, clock.now, true)
	upgraded := &countingProbe{engine: Engine{Kind: KindRsync, Version: "3.4.4", Protocol: 32}}
	got, err := bypass.cached(context.Background(), "studio", upgraded.run)
	if err != nil {
		t.Fatalf("bypass error = %v", err)
	}
	if upgraded.calls != 1 {
		t.Errorf("probe ran %d times with --no-probe-cache, want 1", upgraded.calls)
	}
	if !got.DeltaUsable() {
		t.Errorf("bypass returned %+v, want the freshly probed engine", got)
	}

	// And it must have written the new answer back, so the next normal run sees
	// the upgrade rather than the stale entry it just corrected.
	after := newProberAt(path, clock.now, false)
	never := &countingProbe{engine: Engine{Kind: KindAbsent}}
	cached, err := after.cached(context.Background(), "studio", never.run)
	if err != nil {
		t.Fatalf("post-bypass read error = %v", err)
	}
	if never.calls != 0 {
		t.Errorf("probe ran %d times after a bypass refresh, want 0", never.calls)
	}
	if !cached.DeltaUsable() || cached.Version != "3.4.4" {
		t.Errorf("cache holds %+v, want the refreshed 3.4.4 engine", cached)
	}
}

func TestCacheIsPerMachine(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	p, _ := newTestProber(t, clock, false)

	studio := &countingProbe{engine: Engine{Kind: KindRsync, Protocol: 32}}
	laptop := &countingProbe{engine: Engine{Kind: KindOpenRsync, Protocol: 29}}

	if _, err := p.cached(context.Background(), "studio", studio.run); err != nil {
		t.Fatal(err)
	}
	got, err := p.cached(context.Background(), "laptop", laptop.run)
	if err != nil {
		t.Fatal(err)
	}
	if laptop.calls != 1 {
		t.Errorf("laptop probe ran %d times, want 1 — one machine's entry must not answer for another", laptop.calls)
	}
	if got.Kind != KindOpenRsync {
		t.Errorf("laptop engine = %+v, want its own openrsync result", got)
	}
}

func TestCacheToleratesUnusableFiles(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "corrupt json", content: "{not json"},
		{name: "wrong schema version", content: `{"schema_version":999,"entries":{"studio":{"engine":{"kind":"rsync","protocol":32}}}}`},
		{name: "null entries", content: `{"schema_version":1,"entries":null}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &fakeClock{t: time.Now()}
			path := filepath.Join(t.TempDir(), "rsync-probe.json")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			p := newProberAt(path, clock.now, false)
			probe := &countingProbe{engine: Engine{Kind: KindRsync, Protocol: 32}}

			got, err := p.cached(context.Background(), "studio", probe.run)
			if err != nil {
				t.Fatalf("an unusable cache must not fail the probe, got %v", err)
			}
			if probe.calls != 1 {
				t.Errorf("probe ran %d times, want 1 (unusable cache = cold cache)", probe.calls)
			}
			if !got.DeltaUsable() {
				t.Errorf("engine = %+v, want the freshly probed result", got)
			}
		})
	}
}

func TestCacheWritesSchemaVersionedJSON(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)}
	p, path := newTestProber(t, clock, false)
	probe := &countingProbe{engine: Engine{
		Binary: "/opt/homebrew/bin/rsync", Kind: KindRsync, Version: "3.4.4", Protocol: 32,
	}}
	if _, err := p.cached(context.Background(), "studio", probe.run); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	var file cacheFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("cache is not valid json: %v", err)
	}
	if file.SchemaVersion != cacheSchema {
		t.Errorf("schema_version = %d, want %d", file.SchemaVersion, cacheSchema)
	}
	entry, ok := file.Entries["studio"]
	if !ok {
		t.Fatalf("no entry for studio in %s", raw)
	}
	if entry.Engine.Binary != "/opt/homebrew/bin/rsync" {
		t.Errorf("persisted binary = %q, want the resolved path preserved", entry.Engine.Binary)
	}
	if !entry.ProbedAt.Equal(clock.t) {
		t.Errorf("probed_at = %v, want the injected clock %v", entry.ProbedAt, clock.t)
	}

	// 0600: the file records machine ids and paths, so it follows the same
	// posture as the machines registry it sits beside.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache mode = %o, want 600", perm)
	}
}

func TestCacheDoesNotStoreFailedProbes(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	p, path := newTestProber(t, clock, false)
	failing := &countingProbe{err: camperrors.New("ssh exploded")}

	if _, err := p.cached(context.Background(), "studio", failing.run); err == nil {
		t.Fatal("expected the probe error to propagate")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a failed probe must not be cached; the next run should retry")
	}
}

func TestCachedRespectsContextCancellation(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	p, _ := newTestProber(t, clock, false)
	probe := &countingProbe{engine: Engine{Kind: KindRsync, Protocol: 32}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.cached(ctx, "studio", probe.run); err == nil {
		t.Fatal("cancelled context must not run a probe")
	}
	if probe.calls != 0 {
		t.Errorf("probe ran %d times under a cancelled context, want 0", probe.calls)
	}
}

// A blank HOME is not a home directory. The stdlib resolver accepts it, and
// joining onto it yields a relative path that store would MkdirAll under the
// working directory, scattering ~/.obey state through the user's tree. With no
// resolvable location this cache is simply disabled: a miss, a re-probe, and no
// writes anywhere.
func TestCachePathDisabledWithoutUsableHome(t *testing.T) {
	for _, home := range []string{"", "   "} {
		t.Run("home="+strconv.Quote(home), func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)

			if got := CachePath(); got != "" {
				t.Fatalf("CachePath() = %q, want \"\" (caching disabled)", got)
			}

			p := NewProber(false)
			if _, ok := p.lookup(localKey); ok {
				t.Error("lookup() reported a hit with caching disabled")
			}
			p.store(localKey, Engine{Kind: KindRsync, Protocol: 32})

			entries, err := os.ReadDir(".")
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				t.Errorf("store() wrote %q into the working directory", e.Name())
			}
		})
	}
}
