package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestJumpsConfigNormalizeIntentNavigation(t *testing.T) {
	jumps := &JumpsConfig{
		Paths: CampaignPaths{
			Intents: legacyIntentsPath,
		},
		Shortcuts: map[string]ShortcutConfig{
			"i": {
				Path:        legacyIntentsPath,
				Concept:     "intent",
				Description: "Legacy intents shortcut",
				Source:      ShortcutSourceAuto,
			},
			"api": {
				Path:        "projects/api/",
				Description: "Custom API shortcut",
				Source:      ShortcutSourceUser,
			},
		},
	}

	if changed := jumps.NormalizeIntentNavigation(); !changed {
		t.Fatal("NormalizeIntentNavigation() = false, want true for legacy intent values")
	}

	if got := jumps.Paths.Intents; got != ".campaign/intents/" {
		t.Fatalf("Paths.Intents = %q, want %q", got, ".campaign/intents/")
	}

	intentShortcut, ok := jumps.Shortcuts["i"]
	if !ok {
		t.Fatal("intent shortcut i should be present after normalization")
	}
	if intentShortcut != DefaultNavigationShortcuts()["i"] {
		t.Fatalf("shortcut i = %#v, want %#v", intentShortcut, DefaultNavigationShortcuts()["i"])
	}

	apiShortcut, ok := jumps.Shortcuts["api"]
	if !ok {
		t.Fatal("custom api shortcut should be preserved")
	}
	if apiShortcut.Path != "projects/api/" {
		t.Fatalf("api shortcut path = %q, want %q", apiShortcut.Path, "projects/api/")
	}
}

func TestLoadJumpsConfig_NormalizesLegacyIntentNavigationAndPersists(t *testing.T) {
	root := t.TempDir()
	settingsDir := SettingsDirPath(root)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("failed to create settings dir: %v", err)
	}

	raw := `
paths:
  workflow: workflow/
  intents: workflow/intents/
shortcuts:
  i:
    path: workflow/intents/
    concept: intent
    description: Legacy intents
    source: auto
  api:
    path: projects/api/
    description: Custom API shortcut
    source: user
`
	configPath := filepath.Join(settingsDir, JumpsConfigFile)
	if err := os.WriteFile(configPath, []byte(raw), 0644); err != nil {
		t.Fatalf("failed to write jumps config: %v", err)
	}

	ctx := context.Background()
	cfg, err := LoadJumpsConfig(ctx, root)
	if err != nil {
		t.Fatalf("LoadJumpsConfig() error = %v", err)
	}

	if got := cfg.Paths.Intents; got != ".campaign/intents/" {
		t.Fatalf("Paths.Intents = %q, want %q", got, ".campaign/intents/")
	}

	intentShortcut, ok := cfg.Shortcuts["i"]
	if !ok {
		t.Fatal("intent shortcut i should be present after load")
	}
	if intentShortcut.Path != ".campaign/intents/" {
		t.Fatalf("shortcut i path = %q, want %q", intentShortcut.Path, ".campaign/intents/")
	}

	persistedData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read persisted jumps config: %v", err)
	}

	var persisted JumpsConfig
	if err := yaml.Unmarshal(persistedData, &persisted); err != nil {
		t.Fatalf("failed to parse persisted jumps config: %v", err)
	}

	if got := persisted.Paths.Intents; got != ".campaign/intents/" {
		t.Fatalf("persisted Paths.Intents = %q, want %q", got, ".campaign/intents/")
	}

	if got := persisted.Shortcuts["api"].Path; got != "projects/api/" {
		t.Fatalf("persisted api shortcut path = %q, want %q", got, "projects/api/")
	}
}
