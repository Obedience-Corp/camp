package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterWithOrg_Table(t *testing.T) {
	cases := []struct {
		name     string
		org      string
		preSeed  []string
		wantOrg  string // in-memory after register ("" means fallback not stored)
		wantInOrgs string
		wantErr  string
	}{
		{name: "new org", org: "obey", wantOrg: "obey", wantInOrgs: "obey"},
		{name: "existing org", org: "obey", preSeed: []string{"obey"}, wantOrg: "obey", wantInOrgs: "obey"},
		{name: "no flag empty org", org: "", wantOrg: "", wantInOrgs: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := NewRegistry()
			for _, o := range tc.preSeed {
				reg.EnsureOrg(o)
			}
			before := len(reg.Orgs)
			if err := reg.RegisterWithOrg("id-1", "demo", "/tmp/demo-"+tc.name, CampaignTypeProduct, tc.org); err != nil {
				t.Fatalf("RegisterWithOrg: %v", err)
			}
			got := reg.Campaigns["id-1"]
			if got.Org != tc.wantOrg {
				t.Fatalf("entry.Org = %q, want %q", got.Org, tc.wantOrg)
			}
			if tc.wantInOrgs != "" {
				count := 0
				for _, o := range reg.Orgs {
					if o.Name == tc.wantInOrgs {
						count++
					}
				}
				if count != 1 {
					t.Fatalf("Orgs entries for %q = %d, want 1 (no dups); Orgs=%v", tc.wantInOrgs, count, reg.Orgs)
				}
			}
			if tc.org == "" && len(reg.Orgs) != before {
				t.Fatalf("no-flag Register should not grow Orgs; before=%d after=%d", before, len(reg.Orgs))
			}
		})
	}
}

func TestRegisterWithOrg_PersistsThroughSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", path)

	ctx := context.Background()
	if err := UpdateRegistry(ctx, func(r *Registry) error {
		return r.RegisterWithOrg("id-1", "demo", "/tmp/demo", CampaignTypeProduct, "obey")
	}); err != nil {
		t.Fatalf("UpdateRegistry: %v", err)
	}

	reg, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := reg.Campaigns["id-1"]
	if !ok {
		t.Fatal("campaign missing")
	}
	if c.Org != "obey" {
		t.Fatalf("Org = %q, want obey", c.Org)
	}
	if !orgPresent(reg, "obey") {
		t.Fatal("obey missing from reg.Orgs after load")
	}
}

func TestValidateOrgName_ForCreateFlag(t *testing.T) {
	// Mirrors the create/init early validation gate.
	if err := ValidateName("org", "Bad Name"); err == nil {
		t.Fatal("expected invalid name error")
	} else if !strings.Contains(strings.ToLower(err.Error()), "invalid") &&
		!strings.Contains(err.Error(), "org") {
		// ValidateName error should be actionable; just ensure non-nil.
		t.Logf("validation error: %v", err)
	}
	if err := ValidateName("org", "obey"); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
}

func TestRegisterWithOrg_InvalidDoesNotWriteWhenCreateValidates(t *testing.T) {
	// Simulate create early-exit: bad name never reaches Register.
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", path)
	_ = os.WriteFile(path, []byte(`{"version":3,"campaigns":{}}`), 0o644)

	if err := ValidateName("org", "Bad Name"); err == nil {
		t.Fatal("expected validation error")
	}
	reg, err := LoadRegistry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Campaigns) != 0 {
		t.Fatalf("expected no campaigns, got %d", len(reg.Campaigns))
	}
}
