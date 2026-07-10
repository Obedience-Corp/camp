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
)

// machineCompletionTTL bounds how long a warmed per-machine campaign cache is
// offered for completion before it is treated as stale (id-only) again.
const machineCompletionTTL = 60 * time.Second

// machineCacheEntry is the on-disk per-machine completion cache: the remote
// machine's campaign names and when they were fetched. It is a derived cache
// (gitignored), warmed by `camp list --remote`, read on the keystroke path.
type machineCacheEntry struct {
	Campaigns []string `json:"campaigns"`
	FetchedAt int64    `json:"fetched_at"` // unix nanoseconds
}

func (e machineCacheEntry) fresh(now time.Time) bool {
	return now.Sub(time.Unix(0, e.FetchedAt)) <= machineCompletionTTL
}

func machineCacheDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "obey", "cache", "machines")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".obey", "cache", "machines")
}

// readMachineCacheCampaigns returns a machine's cached campaign names only on a
// fresh cache hit. It performs NO ssh — the keystroke path must never block on the
// network. A miss (absent/corrupt/stale) returns (nil, false).
func readMachineCacheCampaigns(id string) ([]string, bool) {
	data, err := os.ReadFile(filepath.Join(machineCacheDir(), id+".json"))
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
func writeMachineCacheCampaigns(id string, campaigns []string) {
	dir := machineCacheDir()
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	data, err := json.Marshal(machineCacheEntry{Campaigns: campaigns, FetchedAt: time.Now().UnixNano()})
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
