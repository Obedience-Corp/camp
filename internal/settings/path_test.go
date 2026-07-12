package settings

import (
	"context"
	"testing"
)

func TestCatalogPath_Local(t *testing.T) {
	tests := []struct {
		name string
		path string
		root string
		want string
	}{
		{"relative stays relative", ".campaign/campaign.yaml", "/home/u/camp", ".campaign/campaign.yaml"},
		{"absolute under root becomes relative", "/home/u/camp/.campaign/x.yaml", "/home/u/camp", ".campaign/x.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := SettingEntry{Scope: ScopeLocal, Path: tt.path}
			if got := CatalogPath(e, tt.root); got != tt.want {
				t.Errorf("CatalogPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestCatalogPath_Global(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	tests := []struct {
		name string
		path string
		want string
	}{
		{"under home collapses to tilde", "/home/u/.obey/campaign/registry.json", "~/.obey/campaign/registry.json"},
		{"exact home is tilde", "/home/u", "~"},
		{"outside home stays absolute", "/etc/obey/registry.json", "/etc/obey/registry.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := SettingEntry{Scope: ScopeGlobal, Path: tt.path}
			if got := CatalogPath(e, ""); got != tt.want {
				t.Errorf("CatalogPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestCatalogPath_GlobalHomeUnset(t *testing.T) {
	t.Setenv("HOME", "")
	e := SettingEntry{Scope: ScopeGlobal, Path: "/home/u/.obey/campaign/registry.json"}
	if got := CatalogPath(e, ""); got != "/home/u/.obey/campaign/registry.json" {
		t.Errorf("CatalogPath with HOME unset = %q, want absolute path unchanged", got)
	}
}

// The global entries carry their resolved real path, so CatalogPath stays
// honest under $CAMP_REGISTRY_PATH and $XDG_CONFIG_HOME overrides (DP4).
func TestCatalogPath_RegistryOverride(t *testing.T) {
	tests := []struct {
		name         string
		home         string
		registryPath string
		want         string
	}{
		{"override under home", "/home/u", "/home/u/custom/registry.json", "~/custom/registry.json"},
		{"override outside home", "/home/u", "/etc/camp/registry.json", "/etc/camp/registry.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", tt.home)
			t.Setenv("CAMP_REGISTRY_PATH", tt.registryPath)
			entries, err := BuildCatalog(context.Background(), "/campaign")
			if err != nil {
				t.Fatalf("BuildCatalog: %v", err)
			}
			e, ok := indexByID(entries)["registry"]
			if !ok {
				t.Fatal("missing registry entry")
			}
			if got := CatalogPath(e, ""); got != tt.want {
				t.Errorf("CatalogPath(registry) = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCatalogPath_GlobalConfigXDGOverride(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	t.Setenv("CAMP_REGISTRY_PATH", "")
	t.Setenv("XDG_CONFIG_HOME", "/home/u/.config")

	entries, err := BuildCatalog(context.Background(), "/campaign")
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	e, ok := indexByID(entries)["global_config"]
	if !ok {
		t.Fatal("missing global_config entry")
	}
	if got := CatalogPath(e, ""); got != "~/.config/obey/campaign/config.json" {
		t.Errorf("CatalogPath(global_config) = %q, want %q", got, "~/.config/obey/campaign/config.json")
	}
}
