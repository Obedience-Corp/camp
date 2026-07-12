package settings

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/config"
	campcontract "github.com/Obedience-Corp/camp/internal/contract"
	"github.com/Obedience-Corp/obey-shared/contract"
)

// BuildCatalog returns every settings entry the TUI knows about: the static
// camp-owned structured entries, the Hidden set derived from the watcher
// contract (so newly-watched managed files are auto-excluded), and the
// hard-coded Secret entries that are never read or listed.
//
// campaignRoot is reserved for future per-campaign entries and to mirror the
// path-helper signatures; the current entries are either campaign-root-relative
// (Local) or machine-resolved (Global), so it is not consulted here.
func BuildCatalog(ctx context.Context, campaignRoot string) ([]SettingEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries := staticEntries()
	entries = append(entries, hiddenFromContract(entries)...)
	entries = append(entries, secretEntries()...)
	return entries, nil
}

// ForScope returns the entries in the given scope that are listable in a menu.
// Hidden and Secret entries are never returned.
func ForScope(entries []SettingEntry, s Scope) []SettingEntry {
	var out []SettingEntry
	for _, e := range entries {
		if e.Scope == s && e.Listable() {
			out = append(out, e)
		}
	}
	return out
}

// staticEntries declares the camp-owned structured settings files. Local paths
// are campaign-root-relative; Global paths are resolved to their real location
// (honoring $CAMP_REGISTRY_PATH and $XDG_CONFIG_HOME via internal/config).
func staticEntries() []SettingEntry {
	return []SettingEntry{
		{
			ID:     "campaign_manifest",
			Title:  "Campaign manifest",
			Desc:   "Campaign identity, mission, type, and concepts.",
			Scope:  ScopeLocal,
			Path:   ".campaign/campaign.yaml",
			Format: FormatYAML,
			Edit:   EditStructured,
			Owner:  "camp",
		},
		{
			ID:     "local_settings",
			Title:  "Local settings",
			Desc:   "Per-campaign preferences for this workspace.",
			Scope:  ScopeLocal,
			Path:   ".campaign/settings/local.json",
			Format: FormatJSON,
			Edit:   EditStructured,
			Owner:  "camp",
		},
		{
			ID:     "allowlist",
			Title:  "Command allowlist",
			Desc:   "Tools this campaign permits agents to run.",
			Scope:  ScopeLocal,
			Path:   ".campaign/settings/allowlist.json",
			Format: FormatJSON,
			Edit:   EditStructured,
			Owner:  "camp",
		},
		{
			ID:     "registry",
			Title:  "Campaign registry",
			Desc:   "Campaigns registered on this machine.",
			Scope:  ScopeGlobal,
			Path:   config.RegistryPath(),
			Format: FormatJSON,
			Edit:   EditStructured,
			Owner:  "camp",
		},
		{
			ID:     "global_config",
			Title:  "Global config",
			Desc:   "Machine-wide camp defaults.",
			Scope:  ScopeGlobal,
			Path:   config.GlobalConfigPath(),
			Format: FormatJSON,
			Edit:   EditStructured,
			Owner:  "camp",
		},
	}
}

// hiddenFromContract derives Hidden entries from the watcher contract so every
// camp-managed file the daemon watches is known to the catalog and never
// surfaced as editable. Files already declared as structured Local entries are
// skipped. This is what keeps the settings surface auto-safe as new watched
// files are added, without a hand-maintained denylist.
func hiddenFromContract(structured []SettingEntry) []SettingEntry {
	declared := make(map[string]bool, len(structured))
	for _, e := range structured {
		if e.Scope == ScopeLocal {
			declared[e.Path] = true
		}
	}

	var hidden []SettingEntry
	for _, ce := range campcontract.CampEntries() {
		if declared[ce.Path] {
			continue
		}
		hidden = append(hidden, SettingEntry{
			ID:     ce.ID,
			Title:  ce.ID,
			Scope:  ScopeLocal,
			Path:   ce.Path,
			Format: formatFromContract(ce.Format),
			Edit:   EditHidden,
			Owner:  ce.Owner,
		})
	}
	return hidden
}

// secretEntries declares files that must never be listed or read from the
// settings TUI. Format is not meaningful for these entries because they are
// never parsed; paths are resolved for display only.
func secretEntries() []SettingEntry {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "~"
	}
	return []SettingEntry{
		{
			ID:     "obey_env",
			Title:  "Obey environment secrets",
			Scope:  ScopeGlobal,
			Path:   filepath.Join(home, ".obey", ".env"),
			Format: FormatYAML,
			Edit:   EditSecret,
			Owner:  "obey",
		},
		{
			ID:     "obey_agent_gh_token",
			Title:  "Obey agent GitHub token",
			Scope:  ScopeGlobal,
			Path:   filepath.Join(home, ".config", "obey-agent", "gh-token"),
			Format: FormatYAML,
			Edit:   EditSecret,
			Owner:  "obey",
		},
	}
}

// formatFromContract maps a watcher-contract format to the catalog's Format.
// Only JSON-family files map to FormatJSON; everything else (yaml, directories,
// markdown) defaults to FormatYAML. Format is informational for Hidden entries,
// which are never parsed by the TUI.
func formatFromContract(f contract.Format) Format {
	switch f {
	case contract.FormatJSON, contract.FormatJSONL:
		return FormatJSON
	default:
		return FormatYAML
	}
}
