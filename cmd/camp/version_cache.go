package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/version"
)

// versionCacheTTL bounds how long a probed remote version is trusted for the
// hop-time warning. Skew changes on install cadence, not on the second, so an
// hours-scale bound is right: short enough that an upgrade stops the warning the
// same day, long enough that the warning survives between diagnose runs. A stale
// entry is simply ignored, so the failure mode is silence, never a wrong claim.
const versionCacheTTL = 12 * time.Hour

// versionCacheEntry is the on-disk per-machine version probe result. Derived,
// gitignored, and disposable: deleting it costs one diagnose.
type versionCacheEntry struct {
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	ProbedAt int64  `json:"probed_at"` // unix nanoseconds
}

func (e versionCacheEntry) fresh(now time.Time) bool {
	return now.Sub(time.Unix(0, e.ProbedAt)) <= versionCacheTTL
}

// versionCachePath keeps the probe beside the completion cache, under the same
// per-machine directory, so one `rm -rf ~/.obey/cache` clears every derived
// artifact camp keeps about a machine.
func versionCachePath(id string) string {
	return filepath.Join(machineCacheDir(), id+".version.json")
}

// writeMachineVersionCache records a probe result. Best-effort: a failure just
// means the next hop does not warn, which is the same as today's behavior.
func writeMachineVersionCache(id, versionStr, commit string) {
	if id == "" || versionStr == "" {
		return
	}
	dir := machineCacheDir()
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	data, err := json.Marshal(versionCacheEntry{Version: versionStr, Commit: commit, ProbedAt: time.Now().UnixNano()})
	if err != nil {
		return
	}
	_ = fsutil.WriteFileAtomically(versionCachePath(id), data, 0o600)
}

// readMachineVersionCache returns a fresh cached probe, or false. Absent,
// corrupt, and stale are all the same answer: no opinion.
func readMachineVersionCache(id string) (versionCacheEntry, bool) {
	data, err := os.ReadFile(versionCachePath(id))
	if err != nil {
		return versionCacheEntry{}, false
	}
	var e versionCacheEntry
	if json.Unmarshal(data, &e) != nil || !e.fresh(time.Now()) {
		return versionCacheEntry{}, false
	}
	return e, true
}

// hopSkewWarning returns the one-line warning for a hop to id, or "" when there
// is nothing to say. It NEVER probes: the hop path must not grow an ssh
// round-trip to deliver an advisory, so it reports only what a previous
// `camp machine diagnose` already learned.
func hopSkewWarning(id string) string {
	entry, ok := readMachineVersionCache(id)
	if !ok {
		return ""
	}
	local := version.Get()
	if !campVersionSkew(local, entry.Version, entry.Commit) {
		return ""
	}
	return "camp on " + id + " is " + entry.Version + ", this machine is " + local.Version +
		"; features added since may not work there (run 'camp machine diagnose " + id + "' to re-check)"
}
