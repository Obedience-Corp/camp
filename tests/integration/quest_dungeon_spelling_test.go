//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initCampaignWithHidden runs `camp init` after pinning the dungeon_hidden
// global setting, so the campaign scaffolds with a known dungeon spelling. It
// deliberately avoids InitCampaign's git commit: these tests use quest
// --no-commit and assert on-disk layout, not git state.
func initCampaignWithHidden(t *testing.T, tc *TestContainer, path, name string, hidden bool) {
	t.Helper()
	cfg := `{"dungeon_hidden": false}`
	if hidden {
		cfg = `{"dungeon_hidden": true}`
	}
	require.NoError(t, tc.WriteGlobalConfig(cfg))
	out, err := tc.RunCamp("init", path, "--name", name, "-d", "d", "-m", "m")
	require.NoError(t, err, "camp init: %s", out)
}

// TestQuestDungeon_HiddenCampaignNeverCreatesVisible is the regression for
// Lance's exact repro: on a hidden campaign, quest scaffolding must reuse the
// hidden .dungeon and never recreate a visible .campaign/quests/dungeon.
func TestQuestDungeon_HiddenCampaignNeverCreatesVisible(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/quest-dungeon-hidden"
	initCampaignWithHidden(t, tc, path, "quest-dungeon-hidden", true)

	// Every one of these runs EnsureScaffold, the code path that recreated the
	// visible dungeon before the fix.
	out, err := tc.RunCampInDir(path, "quest", "create", "alpha", "--no-editor", "--no-commit")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir(path, "quest", "complete", "alpha", "--no-commit")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir(path, "quest", "create", "beta", "--no-editor", "--no-commit")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir(path, "quest", "use", "beta")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir(path, "quest", "list", "--all")
	require.NoError(t, err, out)

	visible, err := tc.CheckDirExists(path + "/.campaign/quests/dungeon")
	require.NoError(t, err)
	assert.False(t, visible, "visible quest dungeon must never be created on a hidden campaign")

	hidden, err := tc.CheckDirExists(path + "/.campaign/quests/.dungeon")
	require.NoError(t, err)
	assert.True(t, hidden, "hidden quest dungeon should be present")

	// The completed quest lives under the hidden dungeon and is listed.
	completedDir, err := tc.CheckDirExists(path + "/.campaign/quests/.dungeon/completed")
	require.NoError(t, err)
	assert.True(t, completedDir, "completed bucket should exist under .dungeon")

	listOut, err := tc.RunCampInDir(path, "quest", "list", "--all", "--json")
	require.NoError(t, err, listOut)
	assert.Contains(t, listOut, "alpha", "completed quest in .dungeon must appear in list --all")
	assert.Contains(t, listOut, `"status": "completed"`)
}

// TestQuestDungeon_MigratedHiddenQuestIsVisible reproduces the second symptom:
// a quest that already lives in .campaign/quests/.dungeon (as after a dungeon
// migrate) is invisible to the quest service, which read the hardcoded visible
// path. After the fix it appears in listings, and quest commands still never
// create a visible dungeon beside it.
func TestQuestDungeon_MigratedHiddenQuestIsVisible(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/quest-dungeon-migrated"
	initCampaignWithHidden(t, tc, path, "quest-dungeon-migrated", true)

	questYAML := "id: qst_20260629_migold\n" +
		"name: migrated-old\n" +
		"status: completed\n" +
		"created_at: 2026-06-29T00:00:00Z\n" +
		"updated_at: 2026-06-29T00:00:00Z\n"
	require.NoError(t, tc.WriteFile(
		path+"/.campaign/quests/.dungeon/completed/20260629-migrated-old/quest.yaml", questYAML))

	// The migrated quest, filed under .dungeon, must be visible again in listings.
	listOut, err := tc.RunCampInDir(path, "quest", "list", "--all", "--json")
	require.NoError(t, err, listOut)
	assert.Contains(t, listOut, "migrated-old", "migrated hidden-dungeon quest must be visible in list --all")

	// Lance's exact repro: creating and using a quest reran EnsureScaffold and
	// recreated a visible dungeon beside the hidden one. It must not anymore.
	out, err := tc.RunCampInDir(path, "quest", "create", "fresh", "--no-editor", "--no-commit")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir(path, "quest", "use", "fresh")
	require.NoError(t, err, out)

	visible, err := tc.CheckDirExists(path + "/.campaign/quests/dungeon")
	require.NoError(t, err)
	assert.False(t, visible, "quest commands must not recreate a visible dungeon beside .dungeon")
}

// TestQuestDungeon_LegacyCampaignKeepsVisible confirms the fix honors an
// established visible spelling rather than forcing hidden: a legacy campaign
// keeps completing quests into the visible dungeon and never grows a .dungeon.
func TestQuestDungeon_LegacyCampaignKeepsVisible(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/quest-dungeon-legacy"
	initCampaignWithHidden(t, tc, path, "quest-dungeon-legacy", false)

	out, err := tc.RunCampInDir(path, "quest", "create", "gamma", "--no-editor", "--no-commit")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir(path, "quest", "complete", "gamma", "--no-commit")
	require.NoError(t, err, out)

	visibleCompleted, err := tc.CheckDirExists(path + "/.campaign/quests/dungeon/completed")
	require.NoError(t, err)
	assert.True(t, visibleCompleted, "legacy campaign should keep using the visible dungeon")

	hidden, err := tc.CheckDirExists(path + "/.campaign/quests/.dungeon")
	require.NoError(t, err)
	assert.False(t, hidden, "legacy campaign must not grow a hidden dungeon")

	listOut, err := tc.RunCampInDir(path, "quest", "list", "--all", "--json")
	require.NoError(t, err, listOut)
	assert.Contains(t, listOut, "gamma")
}
