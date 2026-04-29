//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntentList_MigratesLegacyIntentRootAndAudit(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/intent-migrate-root"

	_, err := tc.InitCampaign(path, "intent-migrate-root", "product")
	require.NoError(t, err)

	legacyObey := "# legacy intent docs\n"
	legacyAudit := "{\"event\":\"create\"}\n"
	inboxID := "20260316-legacy-inbox"
	doneID := "20260316-legacy-done"

	require.NoError(t, tc.WriteFile(path+"/workflow/intents/OBEY.md", legacyObey))
	require.NoError(t, tc.WriteFile(path+"/workflow/intents/.intents.jsonl", legacyAudit))
	require.NoError(t, tc.WriteFile(
		fmt.Sprintf("%s/workflow/intents/inbox/%s.md", path, inboxID),
		intentContent(inboxID, "Legacy Inbox", "inbox"),
	))
	require.NoError(t, tc.WriteFile(
		fmt.Sprintf("%s/workflow/intents/done/%s.md", path, doneID),
		intentContent(doneID, "Legacy Done", "done"),
	))

	output, err := tc.RunCampInDir(path, "intent", "list")
	require.NoError(t, err, "intent list should migrate legacy intent state:\n%s", output)

	inboxExists, err := tc.CheckFileExists(fmt.Sprintf("%s/.campaign/intents/inbox/%s.md", path, inboxID))
	require.NoError(t, err)
	assert.True(t, inboxExists, "legacy inbox intent should move into the canonical root")

	donePath := fmt.Sprintf("%s/.campaign/intents/dungeon/done/%s.md", path, doneID)
	doneExists, err := tc.CheckFileExists(donePath)
	require.NoError(t, err)
	assert.True(t, doneExists, "legacy done intent should move into dungeon/done")

	doneContent, err := tc.ReadFile(donePath)
	require.NoError(t, err)
	assert.Contains(t, doneContent, "status: dungeon/done", "migrated done intent should retain the canonical done status")

	canonicalAudit, err := tc.ReadFile(path + "/.campaign/intents/.intents.jsonl")
	require.NoError(t, err)
	assert.Equal(t, legacyAudit, canonicalAudit, "audit log should be migrated into the canonical root")

	canonicalObey, err := tc.ReadFile(path + "/.campaign/intents/OBEY.md")
	require.NoError(t, err)
	assert.Equal(t, legacyObey, canonicalObey, "legacy marker content should replace the scaffold marker")

	legacyDirExists, err := tc.CheckDirExists(path + "/workflow/intents")
	require.NoError(t, err)
	assert.False(t, legacyDirExists, "legacy workflow/intents root should be removed after migration")
}

func TestIntentList_MigratesLegacyMarkerWithoutIntentState(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/intent-migrate-marker"

	_, err := tc.InitCampaign(path, "intent-migrate-marker", "product")
	require.NoError(t, err)

	legacyObey := "# legacy marker\n"
	require.NoError(t, tc.WriteFile(path+"/workflow/intents/OBEY.md", legacyObey))
	require.NoError(t, tc.WriteFile(path+"/.campaign/intents/OBEY.md", "# scaffold marker\n"))

	output, err := tc.RunCampInDir(path, "intent", "list")
	require.NoError(t, err, "intent list should migrate a marker-only legacy root:\n%s", output)

	canonicalObey, err := tc.ReadFile(path + "/.campaign/intents/OBEY.md")
	require.NoError(t, err)
	assert.Equal(t, legacyObey, canonicalObey, "legacy marker should replace the canonical scaffold marker")

	legacyDirExists, err := tc.CheckDirExists(path + "/workflow/intents")
	require.NoError(t, err)
	assert.False(t, legacyDirExists, "marker-only legacy root should be removed after migration")
}

func TestInitRepair_RemovesLegacyIntentScaffoldResidue(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/intent-repair-scaffold"

	_, err := tc.InitCampaign(path, "intent-repair-scaffold", "product")
	require.NoError(t, err)

	for _, relPath := range []string{
		"inbox/.gitkeep",
		"ready/.gitkeep",
		"active/.gitkeep",
		"dungeon/.gitkeep",
		"dungeon/.crawl.yaml",
	} {
		require.NoError(t, tc.WriteFile(path+"/workflow/intents/"+relPath, "legacy scaffold\n"))
	}

	output, err := tc.RunCampInDir(path, "init", "--repair", "--yes")
	require.NoError(t, err, "camp init --repair should clean duplicate legacy scaffold:\n%s", output)
	assert.Contains(t, output, "Campaign Repaired")

	canonicalDirExists, err := tc.CheckDirExists(path + "/.campaign/intents")
	require.NoError(t, err)
	assert.True(t, canonicalDirExists, "canonical intent root should remain after repair")

	legacyDirExists, err := tc.CheckDirExists(path + "/workflow/intents")
	require.NoError(t, err)
	assert.False(t, legacyDirExists, "legacy workflow/intents scaffold should be removed during repair")
}
