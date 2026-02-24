// Package contract defines the camp CLI's entries for the central watcher contract.
// These entries declare every file and directory that camp manages, so the obey daemon
// knows what to watch via .campaign/watchers.yaml.
package contract

import (
	"github.com/obediencecorp/obey-shared/contract"
)

// CampEntries returns all contract entries owned by the camp CLI.
// These entries are written to .campaign/watchers.yaml via contract.WriteEntries().
// All paths are relative to the campaign root.
func CampEntries() []contract.Entry {
	return []contract.Entry{
		// ============================================================
		// Campaign-Level State (.campaign/)
		// ============================================================

		// Campaign identity and metadata (name, type, description, mission, concepts).
		// Written by camp init, updated by camp settings.
		{
			ID:     "campaign-metadata",
			Path:   ".campaign/campaign.yaml",
			Type:   contract.TypeCampaignMetadata,
			Format: contract.FormatYAML,
			Watch:  contract.WatchFile,
			Owner:  contract.OwnerCamp,
		},

		// Project registry with COCOMO metrics and project list.
		// Written by camp leverage commands.
		{
			ID:     "project-registry",
			Path:   ".campaign/leverage/config.json",
			Type:   contract.TypeProjectRegistry,
			Format: contract.FormatJSON,
			Watch:  contract.WatchFile,
			Owner:  contract.OwnerCamp,
		},

		// User-pinned festivals for quick access in the TUI.
		// Written by camp pin commands.
		{
			ID:     "festival-pins",
			Path:   ".campaign/settings/pins.json",
			Type:   contract.TypeUIPins,
			Format: contract.FormatJSON,
			Watch:  contract.WatchFile,
			Owner:  contract.OwnerCamp,
		},

		// Navigation jumps (shortcuts and paths).
		// Written by camp init, updated by camp settings.
		{
			ID:     "settings-jumps",
			Path:   ".campaign/settings/jumps.yaml",
			Type:   contract.TypeSettingsNavigation,
			Format: contract.FormatYAML,
			Watch:  contract.WatchFile,
			Owner:  contract.OwnerCamp,
		},

		// Tool allowlist for security permissions.
		// Written by camp settings.
		{
			ID:     "settings-allowlist",
			Path:   ".campaign/settings/allowlist.json",
			Type:   contract.TypeSettingsPermissions,
			Format: contract.FormatJSON,
			Watch:  contract.WatchFile,
			Owner:  contract.OwnerCamp,
		},

		// Historical leverage snapshots directory.
		// Each snapshot is a JSON file with metrics at a point in time.
		{
			ID:     "leverage-snapshots",
			Path:   ".campaign/leverage/snapshots/",
			Type:   contract.TypeMetricsSnapshots,
			Format: contract.FormatDirectory,
			Watch:  contract.WatchDirectory,
			Owner:  contract.OwnerCamp,
		},

		// ============================================================
		// Intent Status Directories (workflow/intents/)
		// ============================================================

		// Intents inbox -- newly created intents awaiting triage.
		{
			ID:     "intents-inbox",
			Path:   "workflow/intents/inbox/",
			Type:   contract.TypeIntentStatusDir,
			Format: contract.FormatDirectory,
			Watch:  contract.WatchDirectory,
			Owner:  contract.OwnerCamp,
			Status: "inbox",
		},

		// Intents ready -- triaged intents ready for execution.
		{
			ID:     "intents-ready",
			Path:   "workflow/intents/ready/",
			Type:   contract.TypeIntentStatusDir,
			Format: contract.FormatDirectory,
			Watch:  contract.WatchDirectory,
			Owner:  contract.OwnerCamp,
			Status: "ready",
		},

		// Intents active -- currently executing intents.
		{
			ID:     "intents-active",
			Path:   "workflow/intents/active/",
			Type:   contract.TypeIntentStatusDir,
			Format: contract.FormatDirectory,
			Watch:  contract.WatchDirectory,
			Owner:  contract.OwnerCamp,
			Status: "active",
		},

		// Intents dungeon -- archived/completed intents.
		{
			ID:     "intents-dungeon",
			Path:   "workflow/intents/dungeon/",
			Type:   contract.TypeIntentStatusDir,
			Format: contract.FormatDirectory,
			Watch:  contract.WatchDirectory,
			Owner:  contract.OwnerCamp,
			Status: "archived",
		},

		// ============================================================
		// Workflow Configuration
		// ============================================================

		// Design workflow status config.
		// Defines status directory semantics for the design workflow.
		{
			ID:     "workflow-design-config",
			Path:   "workflow/design/.workflow.yaml",
			Type:   contract.TypeWorkflowConfig,
			Format: contract.FormatYAML,
			Watch:  contract.WatchFile,
			Owner:  contract.OwnerCamp,
		},
	}
}
