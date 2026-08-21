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
// same day, long enough that the warning survives between diagnose runs.
//
// A TTL-expired, absent, or corrupt entry is ignored, so those failure modes are
// silence rather than a wrong claim. Within the window the claim can still go
// stale the other way: upgrade the remote to match and the warning persists
// until the next diagnose or expiry. That is the accepted cost of never probing
// on the hop path, and the message points at diagnose for exactly this reason.
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
func versionCachePath(id string) (string, bool) {
	dir, ok := machineCacheDir()
	if !ok {
		return "", false
	}
	return filepath.Join(dir, id+".version.json"), true
}

// writeMachineVersionCache records a probe result. Best-effort: a failure just
// means the next hop does not warn, which is the same as today's behavior.
// A failed probe (empty version) is a no-op on purpose — it leaves any prior
// still-fresh entry in place rather than wiping a known-good answer with silence.
func writeMachineVersionCache(id, versionStr, commit string) {
	if id == "" || versionStr == "" {
		return
	}
	dir, ok := machineCacheDir()
	if !ok {
		return
	}
	path, ok := versionCachePath(id)
	if !ok {
		return
	}
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	data, err := json.Marshal(versionCacheEntry{Version: versionStr, Commit: commit, ProbedAt: time.Now().UnixNano()})
	if err != nil {
		return
	}
	_ = fsutil.WriteFileAtomically(path, data, 0o600)
}

// readMachineVersionCache returns a fresh cached probe, or false. Absent,
// corrupt, empty version, and stale are all the same answer: no opinion.
// Empty version is defense in depth: writeMachineVersionCache already refuses
// to record failed probes, but a hand-written or half-written cache file must
// not read as a real answer.
func readMachineVersionCache(id string) (versionCacheEntry, bool) {
	path, ok := versionCachePath(id)
	if !ok {
		return versionCacheEntry{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return versionCacheEntry{}, false
	}
	var e versionCacheEntry
	if json.Unmarshal(data, &e) != nil || e.Version == "" || !e.fresh(time.Now()) {
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
	// Says only that they differ. The skew check does not order versions, and
	// the remote is as likely to be ahead as behind, so naming a direction
	// would be wrong half the time. Reuse the diagnose display helpers so
	// matching versions with different commits (two builds of one tag, or a
	// matching VERSION override) still disambiguate with the commit short hash.
	remoteDisp := campVersionDisplay(machineDiagnoseRow{CampVersion: entry.Version, CampCommit: entry.Commit})
	return "camp on " + id + " is " + remoteDisp + ", this machine is " + campLocalVersionDisplay() +
		"; features may not match (run 'camp machine diagnose " + id + "' to re-check)"
}
