package org

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/spf13/cobra"
)

func mkCampaign(id, name string, last time.Time) config.RegisteredCampaign {
	return config.RegisteredCampaign{ID: id, Name: name, Org: "obey", Status: config.StatusActive, LastAccess: last}
}

func ids(members []config.RegisteredCampaign) []string {
	out := make([]string, len(members))
	for i, m := range members {
		out[i] = m.ID
	}
	return out
}

func TestNextInOrgCycle(t *testing.T) {
	t0 := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	ring := []config.RegisteredCampaign{
		mkCampaign("A-1", "alpha", t0),
		mkCampaign("B-2", "beta", t0),
		mkCampaign("C-3", "gamma", t0),
	}

	tests := []struct {
		name    string
		members []config.RegisteredCampaign
		current string
		wantID  string
		wantOK  bool
	}{
		{"advance from first", ring, "A-1", "B-2", true},
		{"advance from middle", ring, "B-2", "C-3", true},
		{"wrap from last", ring, "C-3", "A-1", true},
		{"current absent falls to first", ring, "Z-9", "A-1", true},
		{"single member has no next", ring[:1], "A-1", "", false},
		{"empty set", nil, "A-1", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := nextInOrgCycle(tt.members, tt.current)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got.ID != tt.wantID {
				t.Errorf("next = %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

func TestMostRecentOther(t *testing.T) {
	early := time.Date(2026, 6, 16, 8, 0, 0, 0, time.UTC)
	mid := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	late := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)

	members := []config.RegisteredCampaign{
		mkCampaign("A-1", "alpha", early),
		mkCampaign("B-2", "beta", late),
		mkCampaign("C-3", "gamma", mid),
	}

	t.Run("picks newest other", func(t *testing.T) {
		got, ok := mostRecentOther(members, "A-1")
		if !ok || got.ID != "B-2" {
			t.Fatalf("got %q ok=%v, want B-2", got.ID, ok)
		}
	})

	t.Run("excludes current even when newest", func(t *testing.T) {
		got, ok := mostRecentOther(members, "B-2")
		if !ok || got.ID != "C-3" {
			t.Fatalf("got %q ok=%v, want C-3", got.ID, ok)
		}
	})

	t.Run("tie broken by name", func(t *testing.T) {
		tied := []config.RegisteredCampaign{
			mkCampaign("X-9", "zeta", late),
			mkCampaign("Y-8", "delta", late),
			mkCampaign("Z-7", "self", early),
		}
		got, ok := mostRecentOther(tied, "Z-7")
		if !ok || got.Name != "delta" {
			t.Fatalf("got %q ok=%v, want delta", got.Name, ok)
		}
	})

	t.Run("no other member", func(t *testing.T) {
		if _, ok := mostRecentOther(members[:1], "A-1"); ok {
			t.Fatal("expected ok=false with only the current campaign")
		}
	})
}

func TestOrderedCycleMembers(t *testing.T) {
	t0 := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	reg := &config.Registry{Campaigns: map[string]config.RegisteredCampaign{
		"B-2": {ID: "B-2", Name: "beta", Org: "obey", Status: config.StatusActive, LastAccess: t0},
		"A-1": {ID: "A-1", Name: "alpha", Org: "obey", Status: config.StatusActive, LastAccess: t0},
		"C-3": {ID: "C-3", Name: "gamma", Org: "obey", Status: config.StatusInactive, LastAccess: t0},
		"D-4": {ID: "D-4", Name: "delta", Org: "other", Status: config.StatusActive, LastAccess: t0},
	}}

	t.Run("active org members sorted by name", func(t *testing.T) {
		got := orderedCycleMembers(reg, cmdutil.CampaignScope{Org: "obey"})
		if want := []string{"A-1", "B-2"}; !equalStrings(ids(got), want) {
			t.Errorf("ids = %v, want %v", ids(got), want)
		}
	})

	t.Run("all includes inactive", func(t *testing.T) {
		got := orderedCycleMembers(reg, cmdutil.CampaignScope{Org: "obey", All: true})
		if want := []string{"A-1", "B-2", "C-3"}; !equalStrings(ids(got), want) {
			t.Errorf("ids = %v, want %v", ids(got), want)
		}
	})

	t.Run("scope excludes other orgs", func(t *testing.T) {
		got := orderedCycleMembers(reg, cmdutil.CampaignScope{Org: "obey", All: true})
		for _, m := range got {
			if m.ID == "D-4" {
				t.Fatal("member from org 'other' leaked into 'obey' cycle")
			}
		}
	})
}

func TestOrderedCycleMembersNameTieBrokenByID(t *testing.T) {
	t0 := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	reg := &config.Registry{Campaigns: map[string]config.RegisteredCampaign{
		"B-2": {ID: "B-2", Name: "dup", Org: "obey", Status: config.StatusActive, LastAccess: t0},
		"A-1": {ID: "A-1", Name: "dup", Org: "obey", Status: config.StatusActive, LastAccess: t0},
	}}
	got := orderedCycleMembers(reg, cmdutil.CampaignScope{Org: "obey"})
	if want := []string{"A-1", "B-2"}; !equalStrings(ids(got), want) {
		t.Errorf("ids = %v, want %v (name tie must break by ID)", ids(got), want)
	}
}

func TestEmitCycleTarget(t *testing.T) {
	from := config.RegisteredCampaign{ID: "A-1", Name: "alpha", Org: "obey", Path: "/camps/alpha"}
	to := config.RegisteredCampaign{ID: "B-2", Name: "beta", Org: "obey", Path: "/camps/needs quote"}

	t.Run("shell-connect quotes the path", func(t *testing.T) {
		var buf bytes.Buffer
		if err := emitCycleTarget(&buf, "next", from, to, cycleFlags{shellConnect: true}); err != nil {
			t.Fatal(err)
		}
		got := buf.String()
		if !strings.HasPrefix(got, "cd -- ") {
			t.Fatalf("shell-connect output %q missing 'cd -- ' prefix", got)
		}
		if strings.Contains(got, "/camps/needs quote\n") {
			t.Fatalf("path with a space was not quoted: %q", got)
		}
	})

	t.Run("print emits path only", func(t *testing.T) {
		var buf bytes.Buffer
		if err := emitCycleTarget(&buf, "next", from, to, cycleFlags{print: true}); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != to.Path+"\n" {
			t.Fatalf("print output = %q, want %q", got, to.Path+"\n")
		}
	})

	t.Run("plain emits cd", func(t *testing.T) {
		var buf bytes.Buffer
		if err := emitCycleTarget(&buf, "next", from, to, cycleFlags{}); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "cd "+to.Path+"\n" {
			t.Fatalf("plain output = %q, want %q", got, "cd "+to.Path+"\n")
		}
	})

	t.Run("json carries source and target", func(t *testing.T) {
		var buf bytes.Buffer
		if err := emitCycleTarget(&buf, "toggle", from, to, cycleFlags{json: true}); err != nil {
			t.Fatal(err)
		}
		var out cycleOutput
		if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if out.SchemaVersion != cycleSchemaVersion {
			t.Errorf("schema = %q, want %q", out.SchemaVersion, cycleSchemaVersion)
		}
		if out.Action != "toggle" {
			t.Errorf("action = %q, want toggle", out.Action)
		}
		if out.From.ID != "A-1" || out.To.ID != "B-2" || out.To.Path != to.Path {
			t.Errorf("unexpected from/to: %+v", out)
		}
	})
}

func TestCycleFlagsConflicts(t *testing.T) {
	newCmd := func(print, jsonOut, shell bool) *cobra.Command {
		c := &cobra.Command{}
		c.Flags().Bool("print", print, "")
		c.Flags().Bool("json", jsonOut, "")
		c.Flags().Bool("all", false, "")
		c.Flags().Bool("shell-connect", shell, "")
		return c
	}

	if _, err := cycleFlagsFrom(newCmd(true, true, false)); err == nil {
		t.Error("expected error for --print with --json")
	}
	if _, err := cycleFlagsFrom(newCmd(true, false, true)); err == nil {
		t.Error("expected error for --shell-connect with --print")
	}
	if _, err := cycleFlagsFrom(newCmd(false, false, true)); err != nil {
		t.Errorf("shell-connect alone should be valid: %v", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
