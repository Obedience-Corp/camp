package contract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/obey-shared/contract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCampEntries_ReturnsNonEmpty(t *testing.T) {
	entries := CampEntries()
	require.NotEmpty(t, entries, "CampEntries must return at least one entry")
}

func TestCampEntries_AllOwnedByCamp(t *testing.T) {
	for _, e := range CampEntries() {
		assert.Equal(t, contract.OwnerCamp, e.Owner,
			"entry %q must be owned by camp", e.ID)
	}
}

func TestCampEntries_UniqueIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, e := range CampEntries() {
		assert.False(t, seen[e.ID], "duplicate entry ID: %s", e.ID)
		seen[e.ID] = true
	}
}

func TestCampEntries_RequiredFieldsPresent(t *testing.T) {
	for _, e := range CampEntries() {
		t.Run(e.ID, func(t *testing.T) {
			assert.NotEmpty(t, e.ID, "ID must not be empty")
			assert.NotEmpty(t, e.Path, "Path must not be empty")
			assert.NotEmpty(t, e.Type, "Type must not be empty")
			assert.NotEmpty(t, string(e.Format), "Format must not be empty")
			assert.NotEmpty(t, string(e.Watch), "Watch must not be empty")
			assert.NotEmpty(t, e.Owner, "Owner must not be empty")
		})
	}
}

func TestCampEntries_ValidFormats(t *testing.T) {
	validFormats := map[contract.Format]bool{
		contract.FormatYAML:      true,
		contract.FormatJSON:      true,
		contract.FormatJSONL:     true,
		contract.FormatDirectory: true,
	}
	for _, e := range CampEntries() {
		assert.True(t, validFormats[e.Format],
			"entry %q has invalid format: %s", e.ID, e.Format)
	}
}

func TestCampEntries_ValidWatchModes(t *testing.T) {
	validModes := map[contract.WatchMode]bool{
		contract.WatchFile:      true,
		contract.WatchDirectory: true,
		contract.WatchAppend:    true,
	}
	for _, e := range CampEntries() {
		assert.True(t, validModes[e.Watch],
			"entry %q has invalid watch mode: %s", e.ID, e.Watch)
	}
}

func TestCampEntries_PassesContractValidation(t *testing.T) {
	c := &contract.Contract{
		Version: contract.ContractVersion,
		Entries: CampEntries(),
	}
	err := contract.Validate(c)
	assert.NoError(t, err, "CampEntries must produce a valid contract")
}

func TestCampEntries_NoTemplateEntries(t *testing.T) {
	for _, e := range CampEntries() {
		assert.False(t, e.Template,
			"camp entry %q must not use templates (camp doesn't use template expansion)", e.ID)
	}
}

func TestCampEntries_IntentStatusDirs(t *testing.T) {
	expectedStatuses := map[string]bool{
		"inbox":    false,
		"ready":    false,
		"active":   false,
		"archived": false,
	}

	for _, e := range CampEntries() {
		if e.Type == contract.TypeIntentStatusDir {
			assert.NotEmpty(t, e.Status, "intent dir entry %q must have Status", e.ID)
			assert.Equal(t, contract.FormatDirectory, e.Format,
				"intent dir entry %q must use FormatDirectory", e.ID)
			assert.Equal(t, contract.WatchDirectory, e.Watch,
				"intent dir entry %q must use WatchDirectory", e.ID)
			if _, ok := expectedStatuses[e.Status]; ok {
				expectedStatuses[e.Status] = true
			}
		}
	}

	for status, found := range expectedStatuses {
		assert.True(t, found, "expected intent status directory for %q not found", status)
	}
}

func TestCampEntries_SpecificPaths(t *testing.T) {
	expectedPaths := map[string]string{
		"campaign-metadata":      ".campaign/campaign.yaml",
		"project-registry":       ".campaign/leverage/config.json",
		"festival-pins":          ".campaign/settings/pins.json",
		"settings-jumps":         ".campaign/settings/jumps.yaml",
		"settings-allowlist":     ".campaign/settings/allowlist.json",
		"leverage-snapshots":     ".campaign/leverage/snapshots/",
		"intents-inbox":          "workflow/intents/inbox/",
		"intents-ready":          "workflow/intents/ready/",
		"intents-active":         "workflow/intents/active/",
		"intents-dungeon":        "workflow/intents/dungeon/",
		"workflow-design-config": "workflow/design/.workflow.yaml",
	}

	entryMap := make(map[string]contract.Entry)
	for _, e := range CampEntries() {
		entryMap[e.ID] = e
	}

	for id, wantPath := range expectedPaths {
		e, ok := entryMap[id]
		require.True(t, ok, "missing entry: %q", id)
		assert.Equal(t, wantPath, e.Path, "entry %q has wrong Path", id)
	}
}

func TestCampEntries_WriteReadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".campaign"), 0o755))

	contractPath := contract.ContractPath(tmpDir)
	entries := CampEntries()
	require.NoError(t, contract.WriteEntries(contractPath, contract.OwnerCamp, entries))

	c, err := contract.Read(contractPath)
	require.NoError(t, err)

	assert.Len(t, c.Entries, len(entries))

	readIDs := make(map[string]bool)
	for _, e := range c.Entries {
		readIDs[e.ID] = true
	}
	for _, e := range entries {
		assert.True(t, readIDs[e.ID], "entry %q missing after round-trip", e.ID)
	}
}

func TestCoexistence_FestThenCamp(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".campaign"), 0o755))
	contractPath := contract.ContractPath(tmpDir)

	festEntries := []contract.Entry{
		{ID: "festival-events", Path: "festivals/.festival/.state/festival_events.jsonl", Type: "festival.lifecycle", Format: contract.FormatJSONL, Watch: contract.WatchAppend, Owner: contract.OwnerFest},
		{ID: "festivals-planning", Path: "festivals/planning/", Type: "festival.status_dir", Format: contract.FormatDirectory, Watch: contract.WatchDirectory, Owner: contract.OwnerFest, Status: "planning"},
		{ID: "festivals-active", Path: "festivals/active/", Type: "festival.status_dir", Format: contract.FormatDirectory, Watch: contract.WatchDirectory, Owner: contract.OwnerFest, Status: "active"},
	}

	require.NoError(t, contract.WriteEntries(contractPath, contract.OwnerFest, festEntries))

	campEntries := CampEntries()
	require.NoError(t, contract.WriteEntries(contractPath, contract.OwnerCamp, campEntries))

	c, err := contract.Read(contractPath)
	require.NoError(t, err)

	assert.Len(t, c.Entries, len(festEntries)+len(campEntries),
		"both fest and camp entries must be present")

	readIDs := make(map[string]bool)
	for _, e := range c.Entries {
		readIDs[e.ID] = true
	}
	for _, e := range festEntries {
		assert.True(t, readIDs[e.ID], "fest entry %q was clobbered by camp", e.ID)
	}
	for _, e := range campEntries {
		assert.True(t, readIDs[e.ID], "camp entry %q missing", e.ID)
	}
}

func TestCoexistence_CampThenFest(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".campaign"), 0o755))
	contractPath := contract.ContractPath(tmpDir)

	campEntries := CampEntries()
	require.NoError(t, contract.WriteEntries(contractPath, contract.OwnerCamp, campEntries))

	festEntries := []contract.Entry{
		{ID: "festival-events", Path: "festivals/.festival/.state/festival_events.jsonl", Type: "festival.lifecycle", Format: contract.FormatJSONL, Watch: contract.WatchAppend, Owner: contract.OwnerFest},
		{ID: "festivals-planning", Path: "festivals/planning/", Type: "festival.status_dir", Format: contract.FormatDirectory, Watch: contract.WatchDirectory, Owner: contract.OwnerFest, Status: "planning"},
	}
	require.NoError(t, contract.WriteEntries(contractPath, contract.OwnerFest, festEntries))

	c, err := contract.Read(contractPath)
	require.NoError(t, err)

	assert.Len(t, c.Entries, len(campEntries)+len(festEntries))

	readIDs := make(map[string]bool)
	for _, e := range c.Entries {
		readIDs[e.ID] = true
	}
	for _, e := range campEntries {
		assert.True(t, readIDs[e.ID], "camp entry %q was clobbered by fest", e.ID)
	}
}

func TestCoexistence_IdempotentRewrite(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".campaign"), 0o755))
	contractPath := contract.ContractPath(tmpDir)

	entries := CampEntries()
	require.NoError(t, contract.WriteEntries(contractPath, contract.OwnerCamp, entries))
	require.NoError(t, contract.WriteEntries(contractPath, contract.OwnerCamp, entries))

	c, err := contract.Read(contractPath)
	require.NoError(t, err)

	assert.Len(t, c.Entries, len(entries),
		"double write must not duplicate entries")
}

func TestCampEntries_NoCrossOwnerIDCollision(t *testing.T) {
	festIDs := map[string]bool{
		"festival-events":             true,
		"festival-types":              true,
		"festival-id-registry":        true,
		"festival-checksums":          true,
		"festival-navigation":         true,
		"festivals-planning":          true,
		"festivals-active":            true,
		"festivals-ready":             true,
		"festivals-ritual":            true,
		"festivals-dungeon-archived":  true,
		"festivals-dungeon-completed": true,
		"festivals-dungeon-someday":   true,
		"festival-metadata":           true,
		"festival-progress":           true,
		"festival-tasks":              true,
	}

	for _, e := range CampEntries() {
		assert.False(t, festIDs[e.ID],
			"camp entry ID %q collides with a fest entry ID", e.ID)
	}
}

func TestCampEntries_ExpectedCount(t *testing.T) {
	entries := CampEntries()
	// 6 campaign state + 4 intent dirs + 1 workflow config = 11
	assert.Len(t, entries, 11, "expected 11 camp entries")
}
