package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
)

func TestLocalSwitchPicksOrderAndScope(t *testing.T) {
	reg := newSwitchScopedRegistry(
		config.RegisteredCampaign{
			ID: "1", Name: "older", Path: "/a", Org: "obey", Status: config.StatusActive,
			LastAccess: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		config.RegisteredCampaign{
			ID: "2", Name: "newer", Path: "/b", Org: "obey", Status: config.StatusActive,
			LastAccess: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		config.RegisteredCampaign{
			ID: "3", Name: "inactive", Path: "/c", Org: "obey", Status: config.StatusInactive,
			LastAccess: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
	)
	picks := localSwitchPicks(reg, cmdutil.CampaignScope{})
	if len(picks) != 2 {
		t.Fatalf("want 2 active locals, got %d", len(picks))
	}
	if picks[0].Local.Name != "newer" || picks[1].Local.Name != "older" {
		t.Errorf("order wrong: %s then %s", picks[0].Local.Name, picks[1].Local.Name)
	}
}

func TestRemoteSwitchPicksSortAndSkipLocal(t *testing.T) {
	rows := []campaignEntry{
		{Name: "z", Machine: "beta"},
		{Name: "a", Machine: "alpha"},
		{Name: "b", Machine: "alpha"},
		{Name: "localish", Machine: "local"},
		{Name: "nopath", Machine: ""},
	}
	picks := remoteSwitchPicks(rows)
	if len(picks) != 3 {
		t.Fatalf("want 3 remote picks, got %d: %+v", len(picks), picks)
	}
	want := []string{"alpha · a", "alpha · b", "beta · z"}
	for i, p := range picks {
		got := switchPickLabel(p, "", false)
		if got != "  "+want[i] {
			t.Errorf("pick %d = %q, want %q", i, got, "  "+want[i])
		}
		if p.Kind != switchPickRemote {
			t.Errorf("pick %d kind = %v, want remote", i, p.Kind)
		}
	}
}

func TestSwitchPickLabelLocalCurrentAndOrg(t *testing.T) {
	local := switchPick{
		Kind:  switchPickLocal,
		Local: config.RegisteredCampaign{Name: "camp", Org: "obey", Path: "/here"},
	}
	if got := switchPickLabel(local, "/here", true); got != "* obey/camp" {
		t.Errorf("current org label = %q", got)
	}
	if got := switchPickLabel(local, "/other", false); got != "  camp" {
		t.Errorf("plain label = %q", got)
	}
	remote := switchPick{Kind: switchPickRemote, Machine: "archdtop", Name: "lance-arch"}
	if got := switchPickLabel(remote, "", true); got != "  archdtop · lance-arch" {
		t.Errorf("remote label = %q", got)
	}
}

func TestUnreachableMachineIDs(t *testing.T) {
	ids := unreachableMachineIDs([]remoteResult{
		{machineID: "b", err: errors.New("x")},
		{machineID: "a", rows: nil},
		{machineID: "c", err: errors.New("y")},
	})
	if len(ids) != 2 || ids[0] != "b" || ids[1] != "c" {
		t.Fatalf("ids = %v, want [b c]", ids)
	}
}

func TestListFilterFromScope(t *testing.T) {
	f := listFilterFromScope(cmdutil.CampaignScope{Org: "obey", All: true})
	if f.org != "obey" || !f.all || f.status != "" {
		t.Errorf("filter = %+v", f)
	}
	f = listFilterFromScope(cmdutil.CampaignScope{Status: "inactive"})
	if f.status != "inactive" || f.all {
		t.Errorf("filter = %+v", f)
	}
}

// TestRemoteLoaderSeam exercises the loader type used by the picker without
// opening a fuzzyfinder UI (no live SSH).
func TestRemoteLoaderSeam(t *testing.T) {
	loader := remoteCampaignLoader(func(_ context.Context, filter listFilter) ([]campaignEntry, []remoteResult, error) {
		if filter.org != "obey" {
			t.Errorf("filter.org = %q", filter.org)
		}
		return []campaignEntry{
				{Name: "lance-arch", Machine: "archdtop", Path: "/r"},
			}, []remoteResult{
				{machineID: "archdtop", rows: []campaignEntry{{Name: "lance-arch", Machine: "archdtop"}}},
				{machineID: "dead", err: errors.New("timeout")},
			}, nil
	})
	rows, results, err := loader(context.Background(), listFilter{org: "obey"})
	if err != nil {
		t.Fatal(err)
	}
	picks := remoteSwitchPicks(rows)
	if len(picks) != 1 || picks[0].Machine != "archdtop" {
		t.Fatalf("picks = %+v", picks)
	}
	if un := unreachableMachineIDs(results); len(un) != 1 || un[0] != "dead" {
		t.Fatalf("unreachable = %v", un)
	}
}
