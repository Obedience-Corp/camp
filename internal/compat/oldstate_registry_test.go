package compat

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/config"
)

const (
	legacyCampaignID = "8deed8b4-0000-4000-8000-0000000000aa"
	secondCampaignID = "8deed8b4-0000-4000-8000-0000000000bb"
)

// stageRegistry points camp's registry resolution at a fixture copy and returns
// its path. It uses CAMP_REGISTRY_PATH, itself a frozen name.
func stageRegistry(t *testing.T, fixture string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	writeFile(t, path, oldStateFixture(t, fixture))
	t.Setenv("CAMP_REGISTRY_PATH", path)
	return path
}

// TestOldStateRegistryPreOrgLoads reads a registry written before orgs, tags,
// and status existed. Those campaigns must come back usable, with the defaults
// synthesized rather than the entries dropped.
func TestOldStateRegistryPreOrgLoads(t *testing.T) {
	stageRegistry(t, "registry-preorg.json")

	reg, err := config.LoadRegistry(requireContext(t))
	if err != nil {
		t.Fatalf("loading pre-org registry: %v", err)
	}

	if got := reg.Len(); got != 2 {
		t.Fatalf("registered camps: got %d, want 2", got)
	}
	entry, ok := reg.GetByID(legacyCampaignID)
	if !ok {
		t.Fatalf("a pre-org campaign must still resolve by id: %s", legacyCampaignID)
	}
	if entry.Name != "legacy-campaign" || entry.Path != "/home/dev/campaigns/legacy-campaign" {
		t.Fatalf("entry identity: got %q at %q", entry.Name, entry.Path)
	}
	if entry.Org != config.DefaultOrg {
		t.Fatalf("pre-org entry org: got %q, want %q", entry.Org, config.DefaultOrg)
	}
	if entry.Status != config.StatusActive {
		t.Fatalf("pre-org entry status: got %q, want %q", entry.Status, config.StatusActive)
	}
	if reg.Version != config.RegistryVersion {
		t.Fatalf("version-less registry should read as current: got %d, want %d", reg.Version, config.RegistryVersion)
	}
}

// TestOldStateRegistryV3Loads pins the shipped registry format, including the
// fields a lookup by name, org, or path depends on.
func TestOldStateRegistryV3Loads(t *testing.T) {
	stageRegistry(t, "registry-v3.json")

	reg, err := config.LoadRegistry(requireContext(t))
	if err != nil {
		t.Fatalf("loading v3 registry: %v", err)
	}

	if reg.Version != 3 {
		t.Fatalf("version: got %d, want 3", reg.Version)
	}
	if reg.DefaultOrg != "obedience" {
		t.Fatalf("default_org: got %q", reg.DefaultOrg)
	}
	entry, ok := reg.GetByName("legacy-campaign")
	if !ok {
		t.Fatal("a v3 campaign must resolve by name")
	}
	if entry.Org != "obedience" || entry.Status != config.StatusReference {
		t.Fatalf("org/status: got %q / %q", entry.Org, entry.Status)
	}
	if len(entry.Tags) != 1 || entry.Tags[0] != "work" {
		t.Fatalf("tags: got %v", entry.Tags)
	}
	if _, ok := reg.FindByPath("/home/dev/campaigns/legacy-campaign"); !ok {
		t.Fatal("a v3 campaign must resolve by path")
	}

	names := orgNames(reg)
	if len(names) != 2 || names[0] != "empty-org" || names[1] != "obedience" {
		t.Fatalf("orgs: got %v, want an org with zero members to survive the load", names)
	}
}

// TestRegistryJSONKeysAreFrozen pins the persisted key set. The registry is
// read by camp, by the Festival app, and by scripts, so a key rename here
// breaks readers that never upgrade in lockstep with the CLI.
func TestRegistryJSONKeysAreFrozen(t *testing.T) {
	entry := config.RegisteredCampaign{
		ID:     legacyCampaignID,
		Name:   "legacy-campaign",
		Path:   "/home/dev/campaigns/legacy-campaign",
		Type:   config.CampaignTypeProduct,
		Org:    "obedience",
		Tags:   []string{"work"},
		Status: config.StatusReference,
	}

	got := mustJSON(t, entry)
	for _, key := range []string{"name", "path", "type", "org", "tags", "status"} {
		if _, ok := got[key]; !ok {
			t.Errorf("registry entry lost the %q key", key)
		}
	}
	if _, ok := got["id"]; ok {
		t.Error("registry entries key on the campaign id; it must not also serialize as a field")
	}

	file := mustJSON(t, config.Registry{
		Version:   config.RegistryVersion,
		Campaigns: map[string]config.RegisteredCampaign{legacyCampaignID: entry},
	})
	for _, key := range []string{"version", "campaigns"} {
		if _, ok := file[key]; !ok {
			t.Errorf("registry file lost the %q top-level key", key)
		}
	}
}

// TestOldStateGlobalConfigLoads pins ~/.obey/campaign/config.json, whose
// campaigns_dir key decides where `camp create` puts a new workspace.
func TestOldStateGlobalConfigLoads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	dir := filepath.Join(home, ".obey", "campaign")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "config.json"), oldStateFixture(t, "config.json"))

	cfg, err := config.LoadGlobalConfig(requireContext(t))
	if err != nil {
		t.Fatalf("loading campaign-era config.json: %v", err)
	}

	if cfg.Editor != "nvim" || !cfg.NoColor || !cfg.Verbose {
		t.Fatalf("preferences dropped: %+v", cfg)
	}
	if cfg.TUI.Theme != "dark" || !cfg.TUI.VimMode {
		t.Fatalf("tui block dropped: %+v", cfg.TUI)
	}
	if cfg.CampaignsDir != "~/campaigns" {
		t.Fatalf("campaigns_dir: got %q", cfg.CampaignsDir)
	}
	if cfg.LedgerWriterID != "w919cf43d" {
		t.Fatalf("ledger_writer_id: got %q", cfg.LedgerWriterID)
	}
	if !cfg.Commit.SyncProjectRefs {
		t.Fatal("commit.sync_project_refs dropped")
	}
	if cfg.ResolveDungeonHidden() {
		t.Fatal("an explicit dungeon_hidden:false must not read as unset")
	}
}

// TestGlobalConfigJSONKeysAreFrozen pins the written key set, so a struct-field
// rename cannot quietly orphan a user's existing preferences.
func TestGlobalConfigJSONKeysAreFrozen(t *testing.T) {
	var cfg config.GlobalConfig
	decodeJSON(t, oldStateFixture(t, "config.json"), &cfg)

	got := mustJSON(t, cfg)
	for _, key := range []string{
		"editor", "no_color", "verbose", "tui",
		"campaigns_dir", "ledger_writer_id", "commit", "dungeon_hidden",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("global config lost the %q key", key)
		}
	}
}

// TestRegistryAndPreferencesYAMLKeysAreFrozen covers the YAML spellings the
// registry and preferences carry alongside their JSON ones. They are a second
// serialization of the same contract, and a rename that only fixed the JSON
// side would leave the two disagreeing.
func TestRegistryAndPreferencesYAMLKeysAreFrozen(t *testing.T) {
	registryYAML, err := yaml.Marshal(config.Registry{
		Version:   config.RegistryVersion,
		Campaigns: map[string]config.RegisteredCampaign{legacyCampaignID: {Name: "legacy-campaign"}},
	})
	if err != nil {
		t.Fatalf("encoding registry as YAML: %v", err)
	}
	if !strings.Contains(string(registryYAML), "campaigns:") {
		t.Errorf("registry YAML lost the campaigns key:\n%s", registryYAML)
	}

	prefsYAML, err := yaml.Marshal(config.GlobalConfig{CampaignsDir: "~/campaigns"})
	if err != nil {
		t.Fatalf("encoding preferences as YAML: %v", err)
	}
	if !strings.Contains(string(prefsYAML), "campaigns_dir:") {
		t.Errorf("preferences YAML lost the campaigns_dir key:\n%s", prefsYAML)
	}
}

func orgNames(reg *config.Registry) []string {
	names := make([]string, 0, len(reg.Orgs))
	for _, o := range reg.Orgs {
		names = append(names, o.Name)
	}
	sort.Strings(names)
	return names
}
