package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/machines"
)

func TestMachineCandidatesFrom(t *testing.T) {
	ms := []machines.Machine{{ID: "devbox"}, {ID: "desktop"}, {ID: "server"}}
	if got := machineCandidatesFrom(ms, ""); !reflect.DeepEqual(got, []string{"devbox:", "desktop:", "server:"}) {
		t.Errorf("no prefix = %v, want all with colon", got)
	}
	if got := machineCandidatesFrom(ms, "de"); !reflect.DeepEqual(got, []string{"devbox:", "desktop:"}) {
		t.Errorf("prefix 'de' = %v, want devbox:/desktop:", got)
	}
	if got := machineCandidatesFrom(ms, "server:"); !reflect.DeepEqual(got, []string{"server:"}) {
		t.Errorf("full 'server:' = %v, want server:", got)
	}
	if got := machineCandidatesFrom(ms, "zzz"); got != nil {
		t.Errorf("non-matching prefix = %v, want nil", got)
	}
}

func TestCompleteMachineSelectorRemoteCacheHitAndMiss(t *testing.T) {
	hit := func(id string) ([]string, bool) {
		return []string{"platform", "planning", "obey"}, true
	}
	miss := func(id string) ([]string, bool) { return nil, false }

	// Cache hit: campaigns matching the remainder, each prefixed with "<id>:"
	// (filterStrings sorts, so planning precedes platform).
	got := completeMachineSelector(nil, cmdutil.CampaignScope{}, "devbox", "pl", hit)
	if !reflect.DeepEqual(got, []string{"devbox:planning", "devbox:platform"}) {
		t.Errorf("cache hit = %v, want devbox:planning/platform", got)
	}
	// Cache miss/unreachable: id only, immediately (no ssh).
	if got := completeMachineSelector(nil, cmdutil.CampaignScope{}, "devbox", "pl", miss); !reflect.DeepEqual(got, []string{"devbox:"}) {
		t.Errorf("cache miss = %v, want [devbox:]", got)
	}
}

func TestPrefixEach(t *testing.T) {
	if got := prefixEach("m:", []string{"a", "b"}); !reflect.DeepEqual(got, []string{"m:a", "m:b"}) {
		t.Errorf("prefixEach = %v", got)
	}
	if got := prefixEach("m:", nil); len(got) != 0 {
		t.Errorf("prefixEach(nil) = %v, want empty", got)
	}
}

func TestMachineCacheEntryFreshness(t *testing.T) {
	now := time.Now()
	fresh := machineCacheEntry{FetchedAt: now.Add(-10 * time.Second).UnixNano()}
	stale := machineCacheEntry{FetchedAt: now.Add(-2 * machineCompletionTTL).UnixNano()}
	if !fresh.fresh(now) {
		t.Error("10s-old entry should be fresh")
	}
	if stale.fresh(now) {
		t.Error("entry older than TTL should be stale")
	}
}
