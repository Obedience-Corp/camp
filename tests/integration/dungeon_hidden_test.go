//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDungeonHidden_FreshInitUsesHiddenDungeon verifies that a brand new
// campaign scaffolds ".dungeon" (hidden) at every standard location when the
// system-level dungeon_hidden setting is unset (its default is true).
func TestDungeonHidden_FreshInitUsesHiddenDungeon(t *testing.T) {
	tc := GetSharedContainer(t)

	// Reset() seeds dungeon_hidden=false for the rest of this suite; clear
	// the config entirely so this test observes the real unset default.
	require.NoError(t, tc.WriteGlobalConfig("{}"))

	path := "/campaigns/fresh-hidden"
	output, err := tc.RunCamp("init", path, "--name", "fresh-hidden", "-d", "d", "-m", "m", "--no-git")
	require.NoError(t, err, "camp init should succeed: %s", output)

	for _, dir := range []string{
		path + "/.dungeon",
		path + "/.dungeon/completed",
		path + "/.dungeon/archived",
		path + "/.dungeon/someday",
		path + "/workflow/reviews/.dungeon",
		path + "/workflow/design/.dungeon",
		path + "/workflow/explore/.dungeon",
		path + "/.campaign/intents/.dungeon",
	} {
		exists, err := tc.CheckDirExists(dir)
		require.NoError(t, err)
		assert.True(t, exists, "expected hidden dungeon at %s", dir)
	}

	for _, dir := range []string{
		path + "/dungeon",
		path + "/workflow/reviews/dungeon",
		path + "/workflow/design/dungeon",
		path + "/workflow/explore/dungeon",
		path + "/.campaign/intents/dungeon",
	} {
		exists, err := tc.CheckDirExists(dir)
		require.NoError(t, err)
		assert.False(t, exists, "visible dungeon should not exist at %s when dungeon_hidden defaults to true", dir)
	}
}

// TestDungeonHidden_ExplicitFalseUsesVisibleDungeon documents the opt-out:
// explicitly setting dungeon_hidden=false keeps the legacy visible spelling.
func TestDungeonHidden_ExplicitFalseUsesVisibleDungeon(t *testing.T) {
	tc := GetSharedContainer(t)
	require.NoError(t, tc.WriteGlobalConfig(`{"dungeon_hidden": false}`))

	path := "/campaigns/fresh-visible"
	output, err := tc.RunCamp("init", path, "--name", "fresh-visible", "-d", "d", "-m", "m", "--no-git")
	require.NoError(t, err, "camp init should succeed: %s", output)

	exists, err := tc.CheckDirExists(path + "/dungeon")
	require.NoError(t, err)
	assert.True(t, exists, "expected visible dungeon when dungeon_hidden=false")

	exists, err = tc.CheckDirExists(path + "/.dungeon")
	require.NoError(t, err)
	assert.False(t, exists, "hidden dungeon should not exist when dungeon_hidden=false")
}

// TestDungeonHidden_LegacyCampaignUntouchedWhenSystemDefaultFlips is the core
// backward-compatibility acceptance scenario: a campaign created before the
// hidden-by-default change (visible "dungeon") must keep working exactly as
// before even after the machine's system-level default flips to hidden —
// dungeon, intent, and idea operations must all resolve the established
// visible spelling, and no ".dungeon" may appear alongside it.
func TestDungeonHidden_LegacyCampaignUntouchedWhenSystemDefaultFlips(t *testing.T) {
	tc := GetSharedContainer(t)

	// Create the "legacy" campaign explicitly opted into the visible spelling.
	require.NoError(t, tc.WriteGlobalConfig(`{"dungeon_hidden": false}`))
	path := "/campaigns/legacy-flip"
	_, err := tc.RunCamp("init", path, "--name", "legacy-flip", "-d", "d", "-m", "m", "--no-git")
	require.NoError(t, err)

	exists, err := tc.CheckDirExists(path + "/dungeon")
	require.NoError(t, err)
	require.True(t, exists, "setup: expected legacy campaign to have a visible dungeon")

	// Flip the system default to hidden, simulating an upgraded machine
	// operating on a campaign that predates the change.
	require.NoError(t, tc.WriteGlobalConfig(`{"dungeon_hidden": true}`))

	// camp dungeon list must keep resolving the existing visible dungeon.
	listOut, err := tc.RunCampInDir(path, "dungeon", "list")
	require.NoError(t, err, "dungeon list: %s", listOut)

	// A brand new location that never had a dungeon before follows the
	// *current* system setting (now hidden), per the "new dungeons follow
	// the setting" rule — it has no established spelling of its own to
	// preserve, unlike the campaign's pre-existing standard locations.
	subdir := path + "/workflow/pipelines"
	_, exitCode, err := tc.ExecCommand("mkdir", "-p", subdir)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	addOut, err := tc.RunCampInDir(subdir, "dungeon", "add")
	require.NoError(t, err, "dungeon add: %s", addOut)
	exists, err = tc.CheckDirExists(subdir + "/.dungeon")
	require.NoError(t, err)
	assert.True(t, exists, "a brand new dungeon location should follow the current system setting")
	exists, err = tc.CheckDirExists(subdir + "/dungeon")
	require.NoError(t, err)
	assert.False(t, exists, "a brand new dungeon location should not use the visible spelling once the setting is hidden")

	// camp idea (intent) add/list/move must keep resolving the existing
	// visible intents dungeon.
	addIdeaOut, err := tc.RunCampInDir(path, "idea", "add", "Legacy flip idea")
	require.NoError(t, err, "idea add: %s", addIdeaOut)

	listIdeaOut, err := tc.RunCampInDir(path, "idea", "list")
	require.NoError(t, err, "idea list: %s", listIdeaOut)
	assert.Contains(t, listIdeaOut, "Legacy flip idea")

	files, err := tc.ListDirectory(path + "/.campaign/intents/inbox")
	require.NoError(t, err)
	var idBase string
	for _, f := range files {
		base := f[strings.LastIndex(f, "/")+1:]
		if strings.HasSuffix(base, ".md") {
			idBase = base
			break
		}
	}
	require.NotEmpty(t, idBase, "expected a captured idea markdown file in inbox, got %v", files)
	id := strings.TrimSuffix(idBase, ".md")

	moveOut, err := tc.RunCampInDir(path, "idea", "move", id, "dungeon/done", "--reason", "flip test")
	require.NoError(t, err, "idea move: %s", moveOut)

	exists, err = tc.CheckFileExists(path + "/.campaign/intents/dungeon/done/" + idBase)
	require.NoError(t, err)
	assert.True(t, exists, "moved idea should land in the established visible intents dungeon")

	exists, err = tc.CheckDirExists(path + "/.campaign/intents/.dungeon")
	require.NoError(t, err)
	assert.False(t, exists, "no hidden intents dungeon should appear alongside the established visible one")

	// Nothing standard should have been silently rewritten to hidden.
	for _, dir := range []string{
		path + "/.dungeon",
		path + "/workflow/reviews/.dungeon",
		path + "/workflow/design/.dungeon",
		path + "/workflow/explore/.dungeon",
	} {
		exists, err := tc.CheckDirExists(dir)
		require.NoError(t, err)
		assert.False(t, exists, "existing campaign must never gain a hidden dungeon at %s", dir)
	}
}

// TestDungeonHidden_BothSpellingsPrefersVisibleAndWarns covers the explicit
// conflict-resolution rule: when both "dungeon" and ".dungeon" exist under
// the same parent, camp resolves to the visible one and warns on stderr.
func TestDungeonHidden_BothSpellingsPrefersVisibleAndWarns(t *testing.T) {
	tc := GetSharedContainer(t)
	require.NoError(t, tc.WriteGlobalConfig(`{"dungeon_hidden": false}`))

	path := "/campaigns/both-spellings"
	_, err := tc.RunCamp("init", path, "--name", "both-spellings", "-d", "d", "-m", "m", "--no-git")
	require.NoError(t, err)

	// The scaffold already created a visible dungeon; add a conflicting
	// hidden one alongside it to simulate manual meddling or a partial
	// migration.
	_, exitCode, err := tc.ExecCommand("mkdir", "-p", path+"/.dungeon")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(path, "dungeon", "list")
	_ = stdout
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "camp dungeon list should still succeed: %s", stderr)
	assert.Contains(t, stderr, "both dungeon and .dungeon exist", "conflicting spellings should be warned about")
	assert.Contains(t, stderr, "using dungeon", "the warning should state the visible spelling wins")
}

// TestDungeonHidden_IdeaAddWorksHidden exercises "camp idea add" end to end
// against a freshly hidden-dungeon campaign, the direct acceptance scenario
// from the intent for this change.
func TestDungeonHidden_IdeaAddWorksHidden(t *testing.T) {
	tc := GetSharedContainer(t)
	require.NoError(t, tc.WriteGlobalConfig("{}"))

	path := "/campaigns/idea-add-hidden"
	_, err := tc.RunCamp("init", path, "--name", "idea-add-hidden", "-d", "d", "-m", "m", "--no-git")
	require.NoError(t, err)

	addOut, err := tc.RunCampInDir(path, "idea", "add", "Hidden dungeon idea works")
	require.NoError(t, err, "camp idea add: %s", addOut)

	listOut, err := tc.RunCampInDir(path, "idea", "list")
	require.NoError(t, err)
	assert.Contains(t, listOut, "Hidden dungeon idea works")

	// camp intent (the original alias) must resolve to the same storage.
	intentListOut, err := tc.RunCampInDir(path, "intent", "list")
	require.NoError(t, err)
	assert.Contains(t, intentListOut, "Hidden dungeon idea works")
}
