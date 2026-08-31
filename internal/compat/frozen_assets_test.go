package compat

import (
	"io/fs"
	"strings"
	"testing"

	sharedcontract "github.com/Obedience-Corp/obey-shared/contract"

	"github.com/Obedience-Corp/camp/internal/contract"
	"github.com/Obedience-Corp/camp/internal/scaffold"
	"github.com/Obedience-Corp/camp/internal/shell"
)

// TestFrozenWatcherContractPaths pins the paths camp declares to the obey
// daemon. The daemon watches exactly what this list names, so a path that moves
// here stops being watched in every installed daemon, silently.
func TestFrozenWatcherContractPaths(t *testing.T) {
	byPath := make(map[string]sharedcontract.Entry)
	for _, e := range contract.CampEntries() {
		byPath[e.Path] = e
	}

	for _, want := range []string{
		".campaign/campaign.yaml",
		".campaign/settings/jumps.yaml",
		".campaign/settings/pins.json",
		".campaign/settings/allowlist.json",
		".campaign/leverage/config.json",
		".campaign/leverage/snapshots/",
		".campaign/intents/inbox/",
		".campaign/intents/active/",
		".campaign/intents/ready/",
	} {
		if _, ok := byPath[want]; !ok {
			t.Errorf("the watcher contract no longer declares %q", want)
		}
	}

	for path := range byPath {
		if strings.HasPrefix(path, ".camp/") || path == ".camp" {
			t.Errorf("the watcher contract declares %q; .camp is an attachment marker file, not a metadata directory", path)
		}
	}
}

// TestFrozenWatcherEntryTypes pins the two campaign-shaped contract type
// strings. They are read by name on the daemon side.
func TestFrozenWatcherEntryTypes(t *testing.T) {
	if sharedcontract.TypeCampaignMetadata != "campaign.metadata" {
		t.Errorf("contract entry type: got %q, want %q", sharedcontract.TypeCampaignMetadata, "campaign.metadata")
	}
	if sharedcontract.TypeCampaignRegistry != "campaign.registry" {
		t.Errorf("contract entry type: got %q, want %q", sharedcontract.TypeCampaignRegistry, "campaign.registry")
	}

	metadata := entryForPath(t, ".campaign/campaign.yaml")
	if metadata.Type != sharedcontract.TypeCampaignMetadata {
		t.Errorf("campaign.yaml is declared as %q, want %q", metadata.Type, sharedcontract.TypeCampaignMetadata)
	}
}

func entryForPath(t *testing.T, path string) sharedcontract.Entry {
	t.Helper()
	for _, e := range contract.CampEntries() {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("no watcher contract entry for %q", path)
	return sharedcontract.Entry{}
}

// TestFrozenScaffoldSkillBundleNames pins the campaign-named skill directories.
// Agents and existing camps resolve these bundles by name, so the directory is
// an address. Their prose is presentation and does change.
func TestFrozenScaffoldSkillBundleNames(t *testing.T) {
	const skillsDir = "campaign/templates/.campaign/skills"

	entries, err := fs.ReadDir(scaffold.CampaignScaffoldFS, skillsDir)
	if err != nil {
		t.Fatalf("reading scaffolded skill bundles: %v", err)
	}

	present := make(map[string]bool, len(entries))
	for _, e := range entries {
		present[e.Name()] = true
	}

	for _, want := range []string{
		"campaign-commit",
		"campaign-structure",
		"campaign-workflows",
		"cross-campaign",
	} {
		if !present[want] {
			t.Errorf("scaffolded skill bundle %q is gone; the directory name is the address agents resolve", want)
		}
	}
}

// TestFrozenScaffoldTemplateVariable pins campaign_name, the variable every
// scaffolded template interpolates. A renamed variable renders empty rather
// than failing, so a new camp would scaffold a README with a blank title.
func TestFrozenScaffoldTemplateVariable(t *testing.T) {
	manifest, err := fs.ReadFile(scaffold.CampaignScaffoldFS, "campaign/scaffold.yaml")
	if err != nil {
		t.Fatalf("reading scaffold manifest: %v", err)
	}
	if !strings.Contains(string(manifest), "campaign_name:") {
		t.Fatal("the scaffold manifest no longer declares campaign_name")
	}

	var referencing int
	err = fs.WalkDir(scaffold.CampaignScaffoldFS, "campaign/templates", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		data, readErr := fs.ReadFile(scaffold.CampaignScaffoldFS, path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), ".vars.campaign_name") {
			referencing++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking scaffold templates: %v", err)
	}
	if referencing == 0 {
		t.Fatal("no scaffold template interpolates .vars.campaign_name; the template variable was renamed")
	}
}

// TestFrozenShellFunctionNames pins the helpers camp writes into a user's
// shell. They live in an already-sourced rc file, so a rename does not migrate
// anyone: it just stops defining the command they type.
func TestFrozenShellFunctionNames(t *testing.T) {
	for _, shellType := range []string{"bash", "zsh", "fish"} {
		t.Run(shellType, func(t *testing.T) {
			script, err := shell.Generate(shellType)
			if err != nil {
				t.Fatalf("generating %s integration: %v", shellType, err)
			}
			for _, fn := range []string{"cgo", "csw", "corg"} {
				if !strings.Contains(script, fn) {
					t.Errorf("the %s integration no longer defines %s", shellType, fn)
				}
			}
		})
	}
}
