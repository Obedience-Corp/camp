package config

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeRegistryFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", path)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func readFileCampaignKeys(t *testing.T, path string) map[string]map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registry file: %v", err)
	}
	var raw struct {
		Campaigns map[string]map[string]json.RawMessage `json:"campaigns"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal registry file: %v", err)
	}
	return raw.Campaigns
}

func TestLoadRegistry_NoSelfRewriteOnRead(t *testing.T) {
	const fixture = `{
  "version": 2,
  "campaigns": {
    "DEFAULT-0001": {
      "name": "plain",
      "path": "/tmp/plain",
      "type": "campaign",
      "last_access": "2026-06-16T10:00:00Z"
    },
    "ORGANIZED-0002": {
      "name": "organized",
      "path": "/tmp/organized",
      "type": "campaign",
      "last_access": "2026-01-02T10:00:00Z",
      "org": "personal",
      "tags": ["archived-ref"],
      "status": "reference"
    }
  }
}`
	path := writeRegistryFixture(t, fixture)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	if _, err := LoadRegistry(context.Background()); err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("LoadRegistry rewrote the registry on read\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRegistry_DowngradeDropsNewKeys(t *testing.T) {
	const fixture = `{
  "version": 2,
  "campaigns": {
    "ORGANIZED-0001": {
      "name": "organized",
      "path": "/tmp/organized",
      "type": "campaign",
      "last_access": "2026-01-02T10:00:00Z",
      "org": "personal",
      "tags": ["archived-ref"],
      "status": "reference"
    }
  }
}`
	path := writeRegistryFixture(t, fixture)

	type oldCampaign struct {
		Name       string    `json:"name"`
		Path       string    `json:"path"`
		Type       string    `json:"type,omitempty"`
		LastAccess time.Time `json:"last_access,omitempty"`
	}
	type oldFile struct {
		Version   int                    `json:"version"`
		Campaigns map[string]oldCampaign `json:"campaigns"`
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var of oldFile
	if err := json.Unmarshal(data, &of); err != nil {
		t.Fatalf("old binary failed to decode new registry: %v", err)
	}
	out, err := json.MarshalIndent(of, "", "  ")
	if err != nil {
		t.Fatalf("old binary re-encode: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("old binary rewrite: %v", err)
	}

	camps := readFileCampaignKeys(t, path)
	for id, keys := range camps {
		for _, k := range []string{"org", "tags", "status"} {
			if _, has := keys[k]; has {
				t.Errorf("entry %s retained %q after downgrade rewrite; expected intentional key loss", id, k)
			}
		}
	}

	reg, err := LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("LoadRegistry() after downgrade error = %v", err)
	}
	c, ok := reg.Campaigns["ORGANIZED-0001"]
	if !ok {
		t.Fatal("ORGANIZED-0001 missing after downgrade")
	}
	if c.Org != DefaultOrg || c.Status != StatusActive || len(c.Tags) != 0 {
		t.Errorf("downgraded entry did not re-normalize: org=%q status=%q tags=%#v", c.Org, c.Status, c.Tags)
	}
}

func TestLoadRegistry_StatusDefaultsToActive(t *testing.T) {
	const fixture = `{
  "version": 2,
  "campaigns": {
    "NOSTATUS-0001": {
      "name": "nostatus",
      "path": "/tmp/nostatus",
      "type": "campaign",
      "org": "personal"
    }
  }
}`
	writeRegistryFixture(t, fixture)

	reg, err := LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	c, ok := reg.Campaigns["NOSTATUS-0001"]
	if !ok {
		t.Fatal("NOSTATUS-0001 missing")
	}
	if c.Status != StatusActive {
		t.Errorf("Status = %q, want %q", c.Status, StatusActive)
	}
	if c.Org != "personal" {
		t.Errorf("Org = %q, want %q (explicit value must survive)", c.Org, "personal")
	}
}

func TestLoadRegistry_NormalizesMissingKeys(t *testing.T) {
	const fixture = `{
  "version": 2,
  "campaigns": {
    "PLAIN-0001": {
      "name": "plain",
      "path": "/tmp/plain",
      "type": "campaign",
      "last_access": "2026-06-16T10:00:00Z"
    }
  }
}`
	writeRegistryFixture(t, fixture)

	reg, err := LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	c, ok := reg.Campaigns["PLAIN-0001"]
	if !ok {
		t.Fatal("PLAIN-0001 missing")
	}
	if c.Org != DefaultOrg {
		t.Errorf("Org = %q, want %q", c.Org, DefaultOrg)
	}
	if c.Status != StatusActive {
		t.Errorf("Status = %q, want %q", c.Status, StatusActive)
	}
	if c.Tags == nil {
		t.Errorf("Tags = nil, want non-nil empty slice")
	}
	if len(c.Tags) != 0 {
		t.Errorf("len(Tags) = %d, want 0", len(c.Tags))
	}
}

func TestUpdateRegistry_PersistsOnlyMutatedEntry(t *testing.T) {
	const fixture = `{
  "version": 2,
  "campaigns": {
    "MUTATED-0001": {
      "name": "mutated",
      "path": "/tmp/mutated",
      "type": "campaign",
      "last_access": "2026-06-16T10:00:00Z"
    },
    "UNTOUCHED-0002": {
      "name": "untouched",
      "path": "/tmp/untouched",
      "type": "campaign",
      "last_access": "2026-06-16T10:00:00Z"
    }
  }
}`
	path := writeRegistryFixture(t, fixture)

	err := UpdateRegistry(context.Background(), func(reg *Registry) error {
		c := reg.Campaigns["MUTATED-0001"]
		c.Org = "personal"
		reg.Campaigns["MUTATED-0001"] = c
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateRegistry() error = %v", err)
	}

	camps := readFileCampaignKeys(t, path)

	mutated, ok := camps["MUTATED-0001"]
	if !ok {
		t.Fatal("MUTATED-0001 missing on disk")
	}
	got, has := mutated["org"]
	if !has {
		t.Error("mutated entry is missing the org key")
	} else if string(got) != `"personal"` {
		t.Errorf("org = %s, want \"personal\"", got)
	}
	for _, k := range []string{"status", "tags"} {
		if _, has := mutated[k]; has {
			t.Errorf("mutated entry leaked default key %q", k)
		}
	}

	untouched, ok := camps["UNTOUCHED-0002"]
	if !ok {
		t.Fatal("UNTOUCHED-0002 missing on disk")
	}
	for _, k := range []string{"org", "tags", "status"} {
		if _, has := untouched[k]; has {
			t.Errorf("untouched entry gained key %q", k)
		}
	}
}

func TestSaveRegistry_DefaultEntryOmitsKeys(t *testing.T) {
	const fixture = `{
  "version": 2,
  "campaigns": {
    "DEFAULT-0001": {
      "name": "plain",
      "path": "/tmp/plain",
      "type": "campaign",
      "last_access": "2026-06-16T10:00:00Z"
    }
  }
}`
	path := writeRegistryFixture(t, fixture)

	ctx := context.Background()
	reg, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if err := SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	keys := readFileCampaignKeys(t, path)["DEFAULT-0001"]
	if keys == nil {
		t.Fatal("DEFAULT-0001 missing after save")
	}
	for _, k := range []string{"org", "status", "tags"} {
		if v, has := keys[k]; has {
			t.Errorf("default entry persisted %q = %s (want omitted, not null or [])", k, v)
		}
	}
}
