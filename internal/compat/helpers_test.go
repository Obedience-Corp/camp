package compat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// oldStateFixture returns the bytes of one campaign-era artifact. The fixtures
// are literal on-disk files rather than structs marshalled at test time: a
// renamed field would round-trip through a struct and prove nothing.
func oldStateFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "oldstate", name))
	if err != nil {
		t.Fatalf("reading old-state fixture %s: %v", name, err)
	}
	return data
}

// stageOldStateCampaign materializes a campaign-era workspace: .campaign/ with
// campaign.yaml and settings/jumps.yaml, exactly as an older camp wrote them.
func stageOldStateCampaign(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	settings := filepath.Join(root, ".campaign", "settings")
	if err := os.MkdirAll(settings, 0o755); err != nil {
		t.Fatalf("creating .campaign/settings: %v", err)
	}

	writeFile(t, filepath.Join(root, ".campaign", "campaign.yaml"), oldStateFixture(t, "campaign.yaml"))
	writeFile(t, filepath.Join(settings, "jumps.yaml"), oldStateFixture(t, "jumps.yaml"))
	return root
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
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

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s must exist after a round trip: %v", path, err)
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s exists; camp metadata must stay at .campaign/campaign.yaml (docs/terminology.md, Things that must never happen)", path)
	}
}

func requireContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
