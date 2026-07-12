package org

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

// fixtureWithEmptyOrg: alpha in default, beta+gamma in obey, plus empty client-acme.
const fixtureWithEmptyOrg = `{
  "version": 3,
  "orgs": [{"name":"client-acme"},{"name":"obey"}],
  "campaigns": {
    "A-1": {"name":"alpha","path":"/tmp/a","type":"campaign","last_access":"2026-06-16T10:00:00Z"},
    "B-2": {"name":"beta","path":"/tmp/b","type":"campaign","last_access":"2026-06-16T10:00:00Z","org":"obey"},
    "C-3": {"name":"gamma","path":"/tmp/c","type":"campaign","last_access":"2026-06-16T10:00:00Z","org":"obey","status":"inactive","tags":["x"]}
  }
}`

func TestOrgList_IncludesEmptyOrgs(t *testing.T) {
	setOrgRegistry(t, fixtureWithEmptyOrg)
	out, err := execOrg(t, runOrgList, true)
	if err != nil {
		t.Fatalf("org list --json: %v", err)
	}
	var counts []orgCount
	if err := json.Unmarshal([]byte(out), &counts); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	if len(counts) == 0 || counts[0].Org != "default" {
		t.Fatalf("expected fallback-first order, got %+v", counts)
	}
	got := map[string]orgCount{}
	for _, c := range counts {
		got[c.Org] = c
	}
	if got["client-acme"].Campaigns != 0 || got["client-acme"].Active != 0 {
		t.Fatalf("empty org counts = %+v, want 0/0", got["client-acme"])
	}
	if got["obey"].Campaigns != 2 {
		t.Fatalf("obey campaigns = %d, want 2", got["obey"].Campaigns)
	}
	if _, ok := got["client-acme"]; !ok {
		t.Fatal("client-acme missing from org list")
	}
}

func TestOrgShow_EmptyAndAbsent(t *testing.T) {
	setOrgRegistry(t, fixtureWithEmptyOrg)

	out, err := execOrg(t, runOrgShow, false, "client-acme")
	if err != nil {
		t.Fatalf("show empty: %v", err)
	}
	if !strings.Contains(out, "0 campaigns") {
		t.Fatalf("expected 0-member header, got:\n%s", out)
	}

	jsonOut, err := execOrg(t, runOrgShow, true, "client-acme")
	if err != nil {
		t.Fatalf("show empty --json: %v", err)
	}
	if strings.Contains(jsonOut, `"members": null`) {
		t.Fatalf("members is null: %s", jsonOut)
	}
	var result orgShowResult
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Members == nil || len(result.Members) != 0 {
		t.Fatalf("members = %#v, want empty non-nil slice", result.Members)
	}

	if _, err := execOrg(t, runOrgShow, false, "ghost"); err == nil {
		t.Fatal("expected NotFound for absent org")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want not found", err)
	}
}

func TestOrgRename_EmptySourceAndTargetBlock(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(t *testing.T)
		from, to  string
		wantErr   string
		check     func(t *testing.T)
	}{
		{
			name: "empty source renames",
			setup: func(t *testing.T) {
				setOrgRegistry(t, fixtureWithEmptyOrg)
			},
			from: "client-acme",
			to:   "acme",
			check: func(t *testing.T) {
				reg, err := config.LoadRegistry(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if orgExists(reg, "client-acme") {
					t.Fatal("old name still present")
				}
				if !orgExists(reg, "acme") {
					t.Fatal("new name missing")
				}
			},
		},
		{
			name: "existing empty target blocks",
			setup: func(t *testing.T) {
				setOrgRegistry(t, fixtureWithEmptyOrg)
				// acme empty after first rename in isolation: seed both
				_ = config.UpdateRegistry(context.Background(), func(reg *config.Registry) error {
					ensureOrg(reg, "acme")
					return nil
				})
			},
			from:    "obey",
			to:      "acme",
			wantErr: "already exists",
		},
		{
			name: "fallback rename moves DefaultOrg",
			setup: func(t *testing.T) {
				setOrgRegistry(t, fixtureWithEmptyOrg)
			},
			from: "default",
			to:   "personal",
			check: func(t *testing.T) {
				reg, err := config.LoadRegistry(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if reg.FallbackOrg() != "personal" {
					t.Fatalf("FallbackOrg = %q, want personal", reg.FallbackOrg())
				}
				if reg.DefaultOrg != "personal" {
					t.Fatalf("DefaultOrg = %q, want personal", reg.DefaultOrg)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			_, err := execOrg(t, runOrgRename, false, tc.from, tc.to)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q missing %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("rename: %v", err)
			}
			if tc.check != nil {
				tc.check(t)
			}
		})
	}
}
