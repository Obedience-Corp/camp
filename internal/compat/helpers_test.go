package compat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/config"
)

// XDG_CONFIG_HOME resolves to <root>/obey/campaign, so the fixture sits at that
// depth for the real loader to find it.
var globalConfigFixture = filepath.Join("xdg", "obey", "campaign", "config.json")

// The existence check keeps this package read-only: LoadGlobalConfig creates the
// file it cannot find, so a missing fixture would turn these tests into writers.
func oldStateFixturePath(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", "oldstate", name))
	if err != nil {
		t.Fatalf("resolving old-state fixture %s: %v", name, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("old-state fixture %s is missing: %v", name, err)
	}
	return path
}

// oldStateFixture returns the bytes of one campaign-era artifact. The fixtures
// are literal on-disk files rather than structs marshalled at test time: a
// renamed field would round-trip through a struct and prove nothing.
func oldStateFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(oldStateFixturePath(t, name))
	if err != nil {
		t.Fatalf("reading old-state fixture %s: %v", name, err)
	}
	return data
}

func parseOldStateCampaign(t *testing.T) *config.CampaignConfig {
	t.Helper()

	var cfg config.CampaignConfig
	if err := yaml.Unmarshal(oldStateFixture(t, "campaign.yaml"), &cfg); err != nil {
		t.Fatalf("parsing campaign-era campaign.yaml: %v", err)
	}
	cfg.ApplyDefaults()

	var jumps config.JumpsConfig
	if err := yaml.Unmarshal(oldStateFixture(t, "jumps.yaml"), &jumps); err != nil {
		t.Fatalf("parsing campaign-era jumps.yaml: %v", err)
	}
	jumps.NormalizeIntentNavigation()
	jumps.ApplyDefaults()
	cfg.Jumps = &jumps

	if err := config.ValidateCampaignConfig(&cfg); err != nil {
		t.Fatalf("campaign-era metadata no longer validates: %v", err)
	}
	return &cfg
}

func decodeJSON(t *testing.T, data []byte, into any) {
	t.Helper()
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
}

func mustJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("re-decoding: %v", err)
	}
	return out
}

func requireContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
