package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// machineCompletionTTL bounds how long a warmed per-machine campaign cache is
// offered for completion before it is treated as stale (id-only) again.
const machineCompletionTTL = 60 * time.Second

// machineSnapshotTTL bounds a PUSHED snapshot instead. The two differ because
// their refresh events differ: a pulled entry is replaced the next time the user
// runs `camp list --remote`, which is often, while a pushed one is replaced by
// the next hop from that host, which may be tomorrow. A 60s bound would make a
// pushed snapshot unusable within a minute of arriving, i.e. never usable.
//
// The data is campaign NAMES, which change on the order of days, and the cost of
// staleness is a completion suggestion for a renamed campaign. The cost of being
// too short is the feature not working. Asymmetric costs, so this leans long.
const machineSnapshotTTL = 24 * time.Hour

// snapshotSource marks an entry that was pushed to us rather than pulled by us.
const snapshotSource = "push"

// machineCacheEntry is the on-disk per-machine completion cache: the remote
// machine's campaign names and when they were fetched. It is a derived cache
// (gitignored), warmed by `camp list --remote`, read on the keystroke path.
type machineCacheEntry struct {
	Campaigns []string `json:"campaigns"`
	FetchedAt int64    `json:"fetched_at"` // unix nanoseconds
	// Source is "" for entries this machine pulled and "push" for a snapshot a
	// host sent us. It is omitempty and read with a plain json.Unmarshal, so a
	// camp that predates this field ignores it and applies the shorter TTL,
	// degrading to today's behavior rather than erroring.
	Source string `json:"source,omitempty"`
}

func (e machineCacheEntry) fresh(now time.Time) bool {
	return now.Sub(time.Unix(0, e.FetchedAt)) <= e.ttl()
}

func (e machineCacheEntry) ttl() time.Duration {
	if e.Source == snapshotSource {
		return machineSnapshotTTL
	}
	return machineCompletionTTL
}

// machineCacheDir reports where derived per-machine caches live, or false when
// no home directory can be resolved. It returns false rather than a
// working-directory-relative path: this is a throwaway cache on the keystroke
// path, so having no location is a miss, never an error and never a stray
// .obey/ directory in whatever tree the operator happened to be standing in.
func machineCacheDir() (string, bool) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "obey", "cache", "machines"), true
	}
	home, err := pathutil.Home()
	if err != nil {
		return "", false
	}
	return filepath.Join(home, ".obey", "cache", "machines"), true
}

// readMachineCacheCampaigns returns a machine's cached campaign names only on a
// fresh cache hit. It performs NO ssh — the keystroke path must never block on the
// network. A miss (absent/corrupt/stale) returns (nil, false).
func readMachineCacheCampaigns(id string) ([]string, bool) {
	dir, ok := machineCacheDir()
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, false
	}
	var e machineCacheEntry
	if json.Unmarshal(data, &e) != nil || !e.fresh(time.Now()) {
		return nil, false
	}
	return e.Campaigns, true
}

// writeMachineCacheCampaigns warms the cache for id (best-effort; a failure just
// means the next completion is a miss). Called from the `camp list --remote`
// fan-out so real usage keeps completion data fresh without the keystroke path
// ever doing a live ssh.
// writeMachineSnapshotCampaigns records names a HOST pushed to us. It shares the
// file with the pulled path deliberately: one read on the keystroke path stays
// one os.ReadFile, and a pull always overwrites a push because data that came
// from the machine itself is authoritative over another machine's guess.
func writeMachineSnapshotCampaigns(id string, campaigns []string) {
	writeMachineCacheEntry(id, machineCacheEntry{
		Campaigns: campaigns,
		FetchedAt: time.Now().UnixNano(),
		Source:    snapshotSource,
	})
}

func writeMachineCacheCampaigns(id string, campaigns []string) {
	writeMachineCacheEntry(id, machineCacheEntry{Campaigns: campaigns, FetchedAt: time.Now().UnixNano()})
}

func writeMachineCacheEntry(id string, entry machineCacheEntry) {
	dir, ok := machineCacheDir()
	if !ok {
		return
	}
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = fsutil.WriteFileAtomically(filepath.Join(dir, id+".json"), data, 0o600)
}

// machineCandidatesFrom returns "<id>:" completion candidates whose id matches the
// current prefix. Pure (no I/O) so completion logic is host-unit testable.
func machineCandidatesFrom(ms []machines.Machine, prefix string) []string {
	var out []string
	for _, m := range ms {
		if cand := m.ID + ":"; strings.HasPrefix(cand, prefix) {
			out = append(out, cand)
		}
	}
	return out
}

// completeMachineSelector completes the campaign part of a "machine:remainder"
// selector. Local (or "local:") defers to the existing local completion; a remote
// machine reads the warm cache (never ssh) and offers "<id>:<campaign>" on a hit,
// or just "<id>:" on a miss — immediately, no hang. cacheRead is injected for tests.
func completeMachineSelector(
	reg *config.Registry, scope cmdutil.CampaignScope, id, remainder string,
	cacheRead func(string) ([]string, bool),
) []string {
	if id == "" || id == machines.LocalMachineID {
		return prefixEach(id+":", completeSwitchCampaigns(reg, scope, remainder))
	}
	campaigns, ok := cacheRead(id)
	if !ok {
		return []string{id + ":"}
	}
	return prefixEach(id+":", filterStrings(campaigns, remainder))
}

func prefixEach(prefix string, items []string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = prefix + it
	}
	return out
}
