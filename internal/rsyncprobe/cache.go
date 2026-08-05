package rsyncprobe

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// A probe costs an ssh round-trip and a process launch, and the answer changes
// only when someone installs or removes an rsync. Caching it per machine keeps
// the common path free while a TTL bounds how long camp can be wrong about a
// machine the user just upgraded (D002).

const (
	// cacheTTL is how long a probe result is trusted. A day is short enough
	// that installing rsync is noticed the same session, and long enough that
	// a burst of transfers pays for one probe.
	cacheTTL = 24 * time.Hour
	// cacheSchema is bumped whenever the stored shape changes. A file at any
	// other version is discarded rather than migrated: it is derived data that
	// costs one round-trip to rebuild, so migration code would be pure risk
	// for no benefit.
	cacheSchema = 1
	// localKey is the cache key for this machine. It matches the reserved
	// machine id in ~/.obey/machines.yaml, so a real machine can never collide
	// with it.
	localKey = "local"
)

// cacheFile is the on-disk shape of the probe cache.
type cacheFile struct {
	SchemaVersion int                   `json:"schema_version"`
	Entries       map[string]cacheEntry `json:"entries"`
}

// cacheEntry is one machine's remembered probe.
type cacheEntry struct {
	Engine   Engine    `json:"engine"`
	ProbedAt time.Time `json:"probed_at"`
}

// fresh reports whether the entry is still within the TTL as of now.
func (e cacheEntry) fresh(now time.Time) bool {
	return now.Sub(e.ProbedAt) < cacheTTL
}

// CachePath returns the probe cache location, alongside the other
// machine-adjacent state in ~/.obey (XDG_CONFIG_HOME when set, matching
// machines.MachinesPath).
func CachePath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "obey", "rsync-probe.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".obey", "rsync-probe.json")
}

// Prober answers "what rsync runs here and there", remembering results between
// invocations.
type Prober struct {
	// now is injected so TTL behaviour is testable without sleeping.
	now func() time.Time
	// path is the cache file; empty disables caching entirely.
	path string
	// noCache forces a re-probe. The fresh result is still written, so
	// --no-probe-cache repairs a stale entry rather than merely bypassing it.
	noCache bool
}

// NewProber builds a Prober against the default cache location.
func NewProber(noCache bool) *Prober {
	return &Prober{now: time.Now, path: CachePath(), noCache: noCache}
}

// newProberAt builds a Prober against an explicit cache path and clock, for
// tests.
func newProberAt(path string, now func() time.Time, noCache bool) *Prober {
	return &Prober{now: now, path: path, noCache: noCache}
}

// Local returns this machine's engine, cached.
func (p *Prober) Local(ctx context.Context) (Engine, error) {
	return p.cached(ctx, localKey, func() (Engine, error) { return ProbeLocal(ctx) })
}

// Peer returns machineID's engine, cached. src is only consulted on a miss.
func (p *Prober) Peer(ctx context.Context, machineID string, src shellRunner) (Engine, error) {
	return p.cached(ctx, machineID, func() (Engine, error) { return ProbePeer(ctx, src) })
}

// Both returns the pair a transfer needs to decide delta versus whole-file.
func (p *Prober) Both(ctx context.Context, machineID string, src shellRunner) (Pair, error) {
	local, err := p.Local(ctx)
	if err != nil {
		return Pair{}, err
	}
	peer, err := p.Peer(ctx, machineID, src)
	if err != nil {
		return Pair{}, err
	}
	return Pair{Local: local, Peer: peer}, nil
}

// cached returns a fresh cached entry when one exists, otherwise runs probe and
// records the result.
//
// A cache that cannot be read or written is never fatal: the probe still runs
// and the caller still gets a correct answer, just without the saving. Derived
// state must not be able to break the operation it exists to speed up.
func (p *Prober) cached(ctx context.Context, key string, probe func() (Engine, error)) (Engine, error) {
	if ctx.Err() != nil {
		return Engine{}, ctx.Err()
	}
	if !p.noCache {
		if e, ok := p.lookup(key); ok {
			return e, nil
		}
	}
	engine, err := probe()
	if err != nil {
		return engine, err
	}
	p.store(key, engine)
	return engine, nil
}

// lookup returns a cached engine when one is present and still fresh.
func (p *Prober) lookup(key string) (Engine, bool) {
	if p.path == "" {
		return Engine{}, false
	}
	file, err := p.load()
	if err != nil {
		return Engine{}, false
	}
	entry, ok := file.Entries[key]
	if !ok || !entry.fresh(p.now()) {
		return Engine{}, false
	}
	return entry.Engine, true
}

// load reads the cache, treating an absent, unreadable, malformed, or
// wrong-schema file as an empty cache.
func (p *Prober) load() (cacheFile, error) {
	empty := cacheFile{SchemaVersion: cacheSchema, Entries: map[string]cacheEntry{}}
	raw, err := os.ReadFile(p.path)
	if err != nil {
		return empty, err
	}
	var file cacheFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return empty, camperrors.Wrap(err, "parse rsync probe cache")
	}
	if file.SchemaVersion != cacheSchema || file.Entries == nil {
		return empty, nil
	}
	return file, nil
}

// store records an engine for key. Best effort by design: see cached.
func (p *Prober) store(key string, engine Engine) {
	if p.path == "" {
		return
	}
	file, err := p.load()
	if err != nil || file.Entries == nil {
		file = cacheFile{SchemaVersion: cacheSchema, Entries: map[string]cacheEntry{}}
	}
	file.SchemaVersion = cacheSchema
	file.Entries[key] = cacheEntry{Engine: engine, ProbedAt: p.now()}

	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
		return
	}
	_ = fsutil.WriteFileAtomically(p.path, append(raw, '\n'), 0o600)
}
