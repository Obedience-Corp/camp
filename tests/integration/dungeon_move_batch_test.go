//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDungeonMove_DryRunMutatesNothing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-dryrun")

	err := tc.WriteFile(path+"/dungeon/preview-me.md", "# Preview\n")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add item'")
	require.NoError(t, err)

	headBefore := tc.GitOutput(t, path, "rev-parse", "HEAD")

	output, err := tc.RunCampInDir(path, "dungeon", "move", "preview-me.md", "completed", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, output, "Dry run")
	assert.Contains(t, output, "dungeon/completed")

	exists, err := tc.CheckFileExists(path + "/dungeon/preview-me.md")
	require.NoError(t, err)
	assert.True(t, exists, "dry run must not move the item")

	moved, err := checkDatedDungeonStatusItemExists(tc, path+"/dungeon/completed", "preview-me.md")
	require.NoError(t, err)
	assert.False(t, moved, "dry run must not create a destination")

	headAfter := tc.GitOutput(t, path, "rev-parse", "HEAD")
	assert.Equal(t, headBefore, headAfter, "dry run must not create a commit")
}
func TestDungeonMove_DryRunJSON(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-dryrun-json")

	require.NoError(t, tc.WriteFile(path+"/dungeon/a.md", "a\n"))
	require.NoError(t, tc.WriteFile(path+"/dungeon/b.md", "b\n"))

	output, err := tc.RunCampInDir(path, "dungeon", "move", "a.md", "b.md", "completed", "--dry-run", "--json")
	require.NoError(t, err)

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		DryRun        bool   `json:"dry_run"`
		Count         int    `json:"count"`
		Moves         []struct {
			Item        string `json:"item"`
			Destination string `json:"destination"`
			Mode        string `json:"mode"`
		} `json:"moves"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &payload), "dry-run --json must emit valid JSON")
	assert.True(t, payload.DryRun)
	assert.Equal(t, 2, payload.Count)
	require.Len(t, payload.Moves, 2)

	moved, err := checkDatedDungeonStatusItemExists(tc, path+"/dungeon/completed", "a.md")
	require.NoError(t, err)
	assert.False(t, moved, "dry run must not create a destination")
}
func TestDungeonMove_BatchAllOrNothing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-batch-atomic")

	require.NoError(t, tc.WriteFile(path+"/dungeon/keep-me.md", "keep\n"))
	_, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add item'")
	require.NoError(t, err)
	headBefore := tc.GitOutput(t, path, "rev-parse", "HEAD")

	output, err := tc.RunCampInDir(path, "dungeon", "move", "keep-me.md", "missing-item.md", "archived")
	assert.Error(t, err, "batch with an invalid item must fail")
	assert.Contains(t, output, "no moves were applied")
	assert.Contains(t, output, "missing-item.md")

	exists, err := tc.CheckFileExists(path + "/dungeon/keep-me.md")
	require.NoError(t, err)
	assert.True(t, exists, "valid item must not move when a sibling fails validation")

	moved, err := checkDatedDungeonStatusItemExists(tc, path+"/dungeon/archived", "keep-me.md")
	require.NoError(t, err)
	assert.False(t, moved, "no item should reach the destination")

	headAfter := tc.GitOutput(t, path, "rev-parse", "HEAD")
	assert.Equal(t, headBefore, headAfter, "failed batch must not create a commit")
}
func TestDungeonMove_BatchRoundTrip(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-batch-roundtrip")

	for _, name := range []string{"one.md", "two.md", "three.md"} {
		require.NoError(t, tc.WriteFile(path+"/dungeon/"+name, "x\n"))
	}
	_, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add items'")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path, "dungeon", "move", "one.md", "two.md", "three.md", "archived")
	require.NoError(t, err)
	assert.Contains(t, output, "Moved one.md")
	assert.Contains(t, output, "Moved three.md")

	for _, name := range []string{"one.md", "two.md", "three.md"} {
		moved, err := checkDatedDungeonStatusItemExists(tc, path+"/dungeon/archived", name)
		require.NoError(t, err)
		assert.True(t, moved, name+" should be in archived/")

		exists, err := tc.CheckFileExists(path + "/dungeon/" + name)
		require.NoError(t, err)
		assert.False(t, exists, name+" should be removed from dungeon root")
	}

	assertLastDungeonMoveCommit(t, tc, path, "Dungeon sweep: 3 items → archived")
}
func TestDungeonMove_WorkitemDryRun(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-workitem-dryrun")

	output, err := tc.RunCampInDir(path,
		"workitem", "create", "preview-feature",
		"--type", "feature",
		"--title", "Preview feature",
		"--id", "feature-preview-fixed",
	)
	require.NoError(t, err, "workitem create should succeed: %s", output)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add workitem'")
	require.NoError(t, err)
	headBefore := tc.GitOutput(t, path, "rev-parse", "HEAD")

	output, err = tc.RunCampInDir(path, "dungeon", "move", "preview-feature", "archived", "--workitem", "--dry-run")
	require.NoError(t, err, "workitem dry run should succeed: %s", output)
	assert.Contains(t, output, "Dry run")
	assert.Contains(t, output, "workflow/feature/preview-feature")

	exists, err := tc.CheckDirExists(path + "/workflow/feature/preview-feature")
	require.NoError(t, err)
	assert.True(t, exists, "dry run must not move the workitem directory")

	created, err := tc.CheckDirExists(path + "/workflow/feature/dungeon")
	require.NoError(t, err)
	assert.False(t, created, "dry run must not initialize the local dungeon")

	headAfter := tc.GitOutput(t, path, "rev-parse", "HEAD")
	assert.Equal(t, headBefore, headAfter, "dry run must not create a commit")
}
