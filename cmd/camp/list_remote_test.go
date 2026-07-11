package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/machines"
)

func TestRemoteListArgs(t *testing.T) {
	cases := []struct {
		name string
		f    listFilter
		want string
	}{
		{"empty", listFilter{}, "list --json"},
		{"org", listFilter{org: "obey"}, "list --json --org 'obey'"},
		{"tags repeat", listFilter{tags: []string{"a", "b"}}, "list --json --tag 'a' --tag 'b'"},
		{"status", listFilter{status: "inactive"}, "list --json --status 'inactive'"},
		{"all", listFilter{all: true}, "list --json --all"},
		{"combined", listFilter{org: "obey", tags: []string{"x"}, all: true}, "list --json --org 'obey' --tag 'x' --all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := remoteListArgs(tc.f); got != tc.want {
				t.Errorf("remoteListArgs(%+v) = %q, want %q", tc.f, got, tc.want)
			}
		})
	}
}

// TestRemoteRowsReFilteredByOrg proves the local re-filter backstop narrows a
// remote that returns mixed orgs (e.g. a version-skewed camp that ignored the
// forwarded --org) down to the requested org, so `--remote --org X` never leaks
// other orgs from the far machine.
func TestRemoteRowsReFilteredByOrg(t *testing.T) {
	enumerate := func(_ context.Context, m *machines.Machine) ([]campaignEntry, error) {
		return []campaignEntry{
			{ID: "a", Name: "keep", Org: "obey", Machine: m.ID, Status: config.StatusActive},
			{ID: "b", Name: "drop", Org: "other", Machine: m.ID, Status: config.StatusActive},
		}, nil
	}
	ms := []machines.Machine{{ID: "devbox", Host: "h"}}
	results := fanOutRemote(context.Background(), ms, enumerate)

	var combined []campaignEntry
	for _, r := range results {
		if r.err == nil {
			combined = append(combined, r.rows...)
		}
	}
	filtered := filterEntries(combined, listFilter{org: "obey"})
	if len(filtered) != 1 || filtered[0].Org != "obey" || filtered[0].Machine != "devbox" {
		t.Fatalf("org backstop failed, want single obey/devbox row, got %+v", filtered)
	}
}

func TestFanOutRemoteReTagsAndIsolatesFailures(t *testing.T) {
	ms := []machines.Machine{
		{ID: "devbox", Host: "devbox.ts.net"},
		{ID: "server", Host: "server.ts.net"},
		{ID: "dead", Host: "dead.ts.net"},
	}
	enumerate := func(_ context.Context, m *machines.Machine) ([]campaignEntry, error) {
		if m.ID == "dead" {
			return nil, errors.New("dial timeout")
		}
		// The real enumerateRemote re-tags rows with the machine id; the fake
		// returns already-tagged rows to mirror that contract.
		return []campaignEntry{{ID: "c1", Name: m.ID + "-camp", Machine: m.ID}}, nil
	}

	results := fanOutRemote(context.Background(), ms, enumerate)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	byID := map[string]remoteResult{}
	for _, r := range results {
		byID[r.machineID] = r
	}
	if byID["devbox"].err != nil || len(byID["devbox"].rows) != 1 || byID["devbox"].rows[0].Machine != "devbox" {
		t.Errorf("devbox result wrong: %+v", byID["devbox"])
	}
	if byID["dead"].err == nil {
		t.Errorf("dead machine should carry an error, got %+v", byID["dead"])
	}
	// A failed machine must not drop the others.
	if byID["server"].err != nil || len(byID["server"].rows) != 1 {
		t.Errorf("server result dropped by dead machine's failure: %+v", byID["server"])
	}
}

func TestFanOutRemoteConcurrentFixedIndexNoRace(t *testing.T) {
	ms := make([]machines.Machine, 50)
	for i := range ms {
		ms[i] = machines.Machine{ID: string(rune('a'+i%26)) + "-m", Host: "h"}
	}
	var calls atomic.Int64
	enumerate := func(_ context.Context, m *machines.Machine) ([]campaignEntry, error) {
		calls.Add(1)
		return []campaignEntry{{ID: m.ID, Machine: m.ID}}, nil
	}
	results := fanOutRemote(context.Background(), ms, enumerate)
	if len(results) != len(ms) {
		t.Fatalf("results %d, want %d", len(results), len(ms))
	}
	if calls.Load() != int64(len(ms)) {
		t.Fatalf("enumerate called %d times, want %d", calls.Load(), len(ms))
	}
	for i, r := range results {
		if r.machineID != ms[i].ID {
			t.Errorf("index %d: machineID %q != %q (result mis-indexed)", i, r.machineID, ms[i].ID)
		}
	}
}
