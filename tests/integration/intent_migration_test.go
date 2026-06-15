//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntentList_WarnsWithoutMigratingLegacyIntentRootAndAudit(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/intent-read-legacy-root"

	_, err := tc.InitCampaign(path, "intent-read-legacy-root", "product")
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
	require.NoError(t, err, "intent list should warn but not migrate legacy intent state:\n%s", output)
	assert.Contains(t, output, "camp init --repair")
	assert.Contains(t, output, "No intents found.")

	inboxExists, err := tc.CheckFileExists(fmt.Sprintf("%s/.campaign/intents/inbox/%s.md", path, inboxID))
	require.NoError(t, err)
	assert.False(t, inboxExists, "intent list must not move legacy inbox intents into the canonical root")

	donePath := fmt.Sprintf("%s/.campaign/intents/dungeon/done/%s.md", path, doneID)
	doneExists, err := tc.CheckFileExists(donePath)
	require.NoError(t, err)
	assert.False(t, doneExists, "intent list must not move legacy done intents into dungeon/done")

	legacyDonePath := fmt.Sprintf("%s/workflow/intents/done/%s.md", path, doneID)
	doneContent, err := tc.ReadFile(legacyDonePath)
	require.NoError(t, err)
	assert.Contains(t, doneContent, "status: done", "legacy done intent should remain untouched")

	legacyAuditContent, err := tc.ReadFile(path + "/workflow/intents/.intents.jsonl")
	require.NoError(t, err)
	assert.Equal(t, legacyAudit, legacyAuditContent, "legacy audit log should remain in place")

	legacyObeyContent, err := tc.ReadFile(path + "/workflow/intents/OBEY.md")
	require.NoError(t, err)
	assert.Equal(t, legacyObey, legacyObeyContent, "legacy marker content should remain in place")

	legacyDirExists, err := tc.CheckDirExists(path + "/workflow/intents")
	require.NoError(t, err)
	assert.True(t, legacyDirExists, "legacy workflow/intents root should remain until init --repair")
}

func TestIntentList_WarnsWithoutMigratingLegacyMarkerWithoutIntentState(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/intent-read-legacy-marker"

	_, err := tc.InitCampaign(path, "intent-read-legacy-marker", "product")
	require.NoError(t, err)

	legacyObey := "# legacy marker\n"
	require.NoError(t, tc.WriteFile(path+"/workflow/intents/OBEY.md", legacyObey))
	require.NoError(t, tc.WriteFile(path+"/.campaign/intents/OBEY.md", "# scaffold marker\n"))

	output, err := tc.RunCampInDir(path, "intent", "list")
	require.NoError(t, err, "intent list should warn but not migrate a marker-only legacy root:\n%s", output)
	assert.Contains(t, output, "camp init --repair")

	canonicalObey, err := tc.ReadFile(path + "/.campaign/intents/OBEY.md")
	require.NoError(t, err)
	assert.Equal(t, "# scaffold marker\n", canonicalObey, "intent list must not replace the canonical marker")

	legacyObeyContent, err := tc.ReadFile(path + "/workflow/intents/OBEY.md")
	require.NoError(t, err)
	assert.Equal(t, legacyObey, legacyObeyContent, "legacy marker should remain until init --repair")

	legacyDirExists, err := tc.CheckDirExists(path + "/workflow/intents")
	require.NoError(t, err)
	assert.True(t, legacyDirExists, "marker-only legacy root should remain until init --repair")
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
