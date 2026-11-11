//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createCommittedWorkitem creates a workitem of the given type and commits it,
// so promote starts from a clean tree.
func createCommittedWorkitem(t *testing.T, tc *TestContainer, path, slug, typeName, id string) {
	t.Helper()
	output, err := tc.RunCampInDir(path,
		"workitem", "create", slug,
		"--type", typeName,
		"--title", slug,
		"--id", id,
	)
	require.NoError(t, err, "workitem create should succeed: %s", output)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add workitem'")
	require.NoError(t, err)
}

// TestWorkitemPromote_HiddenCampNeverCreatesVisibleTypeDungeon: promoting the
// first workitem of a type into the dungeon must create workflow/<type>/.dungeon
// in a hidden camp, not a visible dungeon/ beside every other .dungeon.
func TestWorkitemPromote_HiddenCampNeverCreatesVisibleTypeDungeon(t *testing.T) {
	tc := GetSharedContainer(t)
	require.NoError(t, tc.WriteGlobalConfig(`{"dungeon_hidden": true}`))

	path := "/campaigns/promote-hidden-type-dungeon"
	_, err := tc.InitCampaign(path, "promote-hidden-type-dungeon", "product")
	require.NoError(t, err)
	createCommittedWorkitem(t, tc, path, "article", "content", "content-article-fixed")

	for _, spelling := range []string{"dungeon", ".dungeon"} {
		exists, err := tc.CheckDirExists(path + "/workflow/content/" + spelling)
		require.NoError(t, err)
		require.False(t, exists, "setup: workflow/content/%s must not exist before promote", spelling)
	}

	output, err := tc.RunCampInDir(path, "workitem", "promote", "content-article-fixed", "--target", "completed", "--no-commit")
	require.NoError(t, err, "promote should succeed: %s", output)

	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/workflow/content/.dungeon/completed", "article")
	require.NoError(t, err)
	assert.True(t, exists, "workitem should land in a new hidden type dungeon")

	visible, err := tc.CheckDirExists(path + "/workflow/content/dungeon")
	require.NoError(t, err)
	assert.False(t, visible, "promote must not create a visible type dungeon in a hidden camp")
}

// TestWorkitemPromote_LegacyVisibleCampKeepsVisibleTypeDungeon: a camp made
// with the visible spelling keeps it for a type's first dungeon even after the
// machine default flips to hidden.
func TestWorkitemPromote_LegacyVisibleCampKeepsVisibleTypeDungeon(t *testing.T) {
	tc := GetSharedContainer(t)
	require.NoError(t, tc.WriteGlobalConfig(`{"dungeon_hidden": false}`))

	path := "/campaigns/promote-visible-type-dungeon"
	_, err := tc.InitCampaign(path, "promote-visible-type-dungeon", "product")
	require.NoError(t, err)
	exists, err := tc.CheckDirExists(path + "/dungeon")
	require.NoError(t, err)
	require.True(t, exists, "setup: legacy camp must have a visible root dungeon")

	require.NoError(t, tc.WriteGlobalConfig(`{"dungeon_hidden": true}`))
	createCommittedWorkitem(t, tc, path, "article", "content", "content-article-fixed")

	output, err := tc.RunCampInDir(path, "workitem", "promote", "content-article-fixed", "--target", "completed", "--no-commit")
	require.NoError(t, err, "promote should succeed: %s", output)

	exists, err = checkDatedDungeonStatusItemExists(tc, path+"/workflow/content/dungeon/completed", "article")
	require.NoError(t, err)
	assert.True(t, exists, "workitem should land in a visible type dungeon like the rest of the camp")

	hidden, err := tc.CheckDirExists(path + "/workflow/content/.dungeon")
	require.NoError(t, err)
	assert.False(t, hidden, "the dungeon_hidden setting must not override a legacy camp's spelling")
}
