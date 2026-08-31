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

// globalConfigFixture is the campaign-era ~/.obey/campaign/config.json. It sits
// inside an XDG-shaped tree so the real loader can be aimed at it with
// XDG_CONFIG_HOME rather than at a home directory the test would have to build.
var globalConfigFixture = filepath.Join("xdg", "obey", "campaign", "config.json")

// oldStateFixturePath returns the path of one campaign-era artifact. Tests that
// point camp's own path resolution at a fixture use this: the loader under test
// opens the file itself, so the fixture has to be addressable rather than
// staged.
//
// The existence check is what keeps this package read-only. A loader aimed at a
// missing file does not simply fail — LoadGlobalConfig, for one, creates the
// file it could not find — so a fixture that went missing would turn these
// tests into writers.
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

// parseOldStateCampaign parses the campaign-era metadata the way
// config.LoadCampaignConfig parses it off disk: campaign.yaml into the config
// struct, then jumps.yaml into the navigation block.
//
// The fixtures are fed as bytes because these assertions are about the YAML
// keys, and this package writes nothing to the filesystem it runs on. That the
// real loader still finds this metadata at .campaign/, and that saving it back
// leaves it there, is pinned against the real binary in
// tests/integration/compat_oldstate_test.go.
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
