package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func withTempRegistryPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", path)
	return path
}

func orgNamesSorted(reg *Registry) []string {
	names := make([]string, 0, len(reg.Orgs))
	for _, o := range reg.Orgs {
		names = append(names, o.Name)
	}
	sort.Strings(names)
	return names
}

func orgPresent(reg *Registry, name string) bool {
	for _, o := range reg.Orgs {
		if o.Name == name {
			return true
		}
	}
	return false
}

func TestRegistryPersistsEmptyOrg(t *testing.T) {
	withTempRegistryPath(t)
	ctx := context.Background()

	if err := UpdateRegistry(ctx, func(r *Registry) error {
		if err := r.Register("camp-1", "alpha", "/tmp/alpha", CampaignTypeProduct); err != nil {
			return err
		}
		// Zero-member non-fallback org must survive the read/write asymmetry trap.
		r.Orgs = append(r.Orgs, OrgEntry{Name: "obey"})
		return nil
	}); err != nil {
		t.Fatalf("UpdateRegistry: %v", err)
	}

	reg, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if !orgPresent(reg, "obey") {
		t.Fatalf("empty org %q missing after round-trip; Orgs=%v", "obey", orgNamesSorted(reg))
	}
}

func TestRegistryFallbackNotPersistedButPresentOnLoad(t *testing.T) {
	path := withTempRegistryPath(t)
	ctx := context.Background()

	reg := NewRegistry()
	if err := reg.Register("camp-1", "alpha", "/tmp/alpha", CampaignTypeProduct); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Include fallback and a real org in memory before save.
	reg.Orgs = []OrgEntry{{Name: reg.FallbackOrg()}, {Name: "obey"}}
	if err := SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read disk: %v", err)
	}
	var disk struct {
		Orgs []OrgEntry `json:"orgs"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal disk: %v", err)
	}
	for _, o := range disk.Orgs {
		if o.Name == DefaultOrg {
			t.Fatalf("fallback %q must not appear in on-disk orgs: %v", DefaultOrg, disk.Orgs)
		}
	}
	if !orgPresentOnDisk(disk.Orgs, "obey") {
		t.Fatalf("expected obey on disk, got %v", disk.Orgs)
	}

	loaded, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if !orgPresent(loaded, DefaultOrg) {
		t.Fatalf("fallback missing after load: %v", orgNamesSorted(loaded))
	}
	if !orgPresent(loaded, "obey") {
		t.Fatalf("obey missing after load: %v", orgNamesSorted(loaded))
	}
}

func orgPresentOnDisk(orgs []OrgEntry, name string) bool {
	for _, o := range orgs {
		if o.Name == name {
			return true
		}
	}
	return false
}

func TestRegistryOrgsLoadSaveIdempotent(t *testing.T) {
	withTempRegistryPath(t)
	ctx := context.Background()

	if err := UpdateRegistry(ctx, func(r *Registry) error {
		if err := r.Register("camp-1", "alpha", "/tmp/alpha", CampaignTypeProduct); err != nil {
			return err
		}
		entry := r.Campaigns["camp-1"]
		entry.Org = "obey"
		r.Campaigns["camp-1"] = entry
		r.Orgs = []OrgEntry{{Name: "obey"}, {Name: "empty-co"}}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	first, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	firstNames := orgNamesSorted(first)

	if err := SaveRegistry(ctx, first); err != nil {
		t.Fatalf("save: %v", err)
	}
	second, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if !reflect.DeepEqual(firstNames, orgNamesSorted(second)) {
		t.Fatalf("orgs not idempotent: first=%v second=%v", firstNames, orgNamesSorted(second))
	}

	if err := SaveRegistry(ctx, second); err != nil {
		t.Fatalf("second save: %v", err)
	}
	third, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("third load: %v", err)
	}
	if !reflect.DeepEqual(firstNames, orgNamesSorted(third)) {
		t.Fatalf("orgs grew/changed on third cycle: first=%v third=%v", firstNames, orgNamesSorted(third))
	}
}

func TestRegistryV2BackfillOrgs(t *testing.T) {
	path := withTempRegistryPath(t)
	ctx := context.Background()

	// Bypass SaveRegistry: legacy v2 file with no top-level orgs key.
	v2 := []byte(`{
  "version": 2,
  "campaigns": {
    "aaaa": {"name": "one", "path": "/tmp/one", "org": "obey"},
    "bbbb": {"name": "two", "path": "/tmp/two"},
    "cccc": {"name": "three", "path": "/tmp/three", "org": "client-acme"}
  }
}`)
	if err := os.WriteFile(path, v2, 0o644); err != nil {
		t.Fatalf("write v2 fixture: %v", err)
	}

	reg, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry v2: %v", err)
	}
	got := orgNamesSorted(reg)
	want := []string{"client-acme", DefaultOrg, "obey"}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("v2 backfill Orgs = %v, want %v", got, want)
	}

	if err := SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	// Save stamps current version.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after save: %v", err)
	}
	var disk struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if disk.Version != RegistryVersion {
		t.Fatalf("version after save = %d, want %d", disk.Version, RegistryVersion)
	}
	if RegistryVersion != 3 {
		t.Fatalf("RegistryVersion = %d, want 3", RegistryVersion)
	}

	reloaded, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reflect.DeepEqual(got, orgNamesSorted(reloaded)) {
		t.Fatalf("after v2 save reload Orgs = %v, want %v", orgNamesSorted(reloaded), got)
	}
}

func TestRegistryOrgsRoundTripFailsWithoutFileField(t *testing.T) {
	// Documents the asymmetry trap: marshaling config.Registry includes Orgs,
	// but LoadRegistry reads through registryfile.File. This test exercises the
	// real Load path and asserts empty orgs survive — which requires Orgs on both.
	withTempRegistryPath(t)
	ctx := context.Background()

	reg := NewRegistry()
	reg.Orgs = []OrgEntry{{Name: "lonely"}}
	if err := SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	loaded, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if !orgPresent(loaded, "lonely") {
		t.Fatal("empty org lost on load — Orgs missing from read or write path")
	}
}
