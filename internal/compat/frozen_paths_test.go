package compat

import (
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/plugin"
)

// frozenPathNames pins the literal spellings from the "Filesystem paths and
// markers" list. A camp that renamed any of these could not read a workspace
// created by the camp already installed on someone's machine.
func TestFrozenPathNames(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"workspace marker directory", config.CampaignDir, ".campaign"},
		{"detection marker directory", campaign.CampaignDir, ".campaign"},
		{"metadata file", config.CampaignConfigFile, "campaign.yaml"},
		{"config org directory", config.OrgName, "obey"},
		{"config app directory", config.AppName, "campaign"},
		{"attachment marker file", campaign.LinkMarkerFile, ".camp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("frozen name changed: got %q, want %q (docs/terminology.md, Frozen technical names)", tt.got, tt.want)
			}
		})
	}
}

// TestFrozenCampaignConfigPath pins the assembled metadata path rather than its
// parts, because a rename that swapped both constants consistently would still
// move every existing camp's metadata file.
func TestFrozenCampaignConfigPath(t *testing.T) {
	root := filepath.Join("home", "dev", "campaigns", "legacy-campaign")
	want := filepath.Join(root, ".campaign", "campaign.yaml")

	if got := config.CampaignConfigPath(root); got != want {
		t.Fatalf("campaign config path: got %q, want %q", got, want)
	}
}

// TestFrozenAttachmentMarkerPath pins where the marker sits: beside a linked
// project root, as a file. Turning .camp into a directory is the specific
// mistake docs/terminology.md calls out as damaging.
func TestFrozenAttachmentMarkerPath(t *testing.T) {
	dir := filepath.Join("home", "dev", "code", "some-project")
	want := filepath.Join(dir, ".camp")

	if got := campaign.MarkerPath(dir); got != want {
		t.Fatalf("attachment marker path: got %q, want %q", got, want)
	}
	if campaign.LinkMarkerVersion != 4 {
		t.Fatalf("attachment marker schema version: got %d, want 4 (a wording change never bumps a schema)", campaign.LinkMarkerVersion)
	}
}

// TestFrozenUserStateLocations pins the three ways camp resolves its own state
// directory, in the order a shipped install already relies on: the explicit
// override, then XDG, then the home directory.
func TestFrozenUserStateLocations(t *testing.T) {
	// Both resolvers only join, so these directories never have to exist.
	home := filepath.Join("/home", "dev")
	xdg := filepath.Join("/home", "dev", ".config")

	t.Run("home", func(t *testing.T) {
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("CAMP_REGISTRY_PATH", "")
		t.Setenv("CAMP_MACHINES_PATH", "")

		assertPath(t, "config dir", mustPath(t, config.ConfigDir), filepath.Join(home, ".obey", "campaign"))
		assertPath(t, "global config", mustPath(t, config.GlobalConfigPath), filepath.Join(home, ".obey", "campaign", "config.json"))
		assertPath(t, "registry", mustPath(t, config.RegistryPath), filepath.Join(home, ".obey", "campaign", "registry.json"))
		assertPath(t, "machines", mustPath(t, machines.MachinesPath), filepath.Join(home, ".obey", "machines.yaml"))
	})

	t.Run("xdg", func(t *testing.T) {
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", xdg)
		t.Setenv("CAMP_REGISTRY_PATH", "")
		t.Setenv("CAMP_MACHINES_PATH", "")

		assertPath(t, "config dir", mustPath(t, config.ConfigDir), filepath.Join(xdg, "obey", "campaign"))
		assertPath(t, "global config", mustPath(t, config.GlobalConfigPath), filepath.Join(xdg, "obey", "campaign", "config.json"))
		assertPath(t, "registry", mustPath(t, config.RegistryPath), filepath.Join(xdg, "obey", "campaign", "registry.json"))
		assertPath(t, "machines", mustPath(t, machines.MachinesPath), filepath.Join(xdg, "obey", "machines.yaml"))
	})

	t.Run("explicit overrides win", func(t *testing.T) {
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", xdg)
		t.Setenv("CAMP_REGISTRY_PATH", "/tmp/pinned-registry.json")
		t.Setenv("CAMP_MACHINES_PATH", "/tmp/pinned-machines.yaml")

		assertPath(t, "registry", mustPath(t, config.RegistryPath), "/tmp/pinned-registry.json")
		assertPath(t, "machines", mustPath(t, machines.MachinesPath), "/tmp/pinned-machines.yaml")
	})
}

// TestFrozenPluginRootEnvName keeps the plugin handshake variable spelled the
// way installed plugins already read it.
func TestFrozenPluginRootEnvName(t *testing.T) {
	if plugin.EnvCampRoot != "CAMP_ROOT" {
		t.Fatalf("plugin root env var: got %q, want %q", plugin.EnvCampRoot, "CAMP_ROOT")
	}
}

func mustPath(t *testing.T, resolve func() (string, error)) string {
	t.Helper()
	got, err := resolve()
	if err != nil {
		t.Fatalf("resolving path: %v", err)
	}
	return got
}

func assertPath(t *testing.T, label, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %q, want %q (docs/terminology.md forbids moving camp state)", label, got, want)
	}
}
