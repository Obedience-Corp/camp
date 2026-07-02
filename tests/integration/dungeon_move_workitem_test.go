//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDungeonMove_WorkitemBySlugToStatus(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-workitem-slug")

	output, err := tc.RunCampInDir(path,
		"workitem", "create", "demo-feature",
		"--type", "feature",
		"--title", "Demo feature",
		"--id", "feature-demo-fixed",
	)
	require.NoError(t, err, "workitem create should succeed: %s", output)

	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add workitem'")
	require.NoError(t, err)

	output, err = tc.RunCampInDir(path, "dungeon", "move", "demo-feature", "archived", "--workitem")
	require.NoError(t, err, "workitem dungeon move should succeed: %s", output)
	assert.Contains(t, output, "Moved demo-feature")
	assert.Contains(t, output, "workflow/feature/demo-feature")
	assert.Contains(t, output, "workflow/feature/dungeon/archived")
	assert.Contains(t, output, "Committed", "should auto-commit")

	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/workflow/feature/dungeon/archived", "demo-feature")
	require.NoError(t, err)
	assert.True(t, exists, "workitem directory should be in local archived dungeon")

	exists, err = tc.CheckDirExists(path + "/workflow/feature/demo-feature")
	require.NoError(t, err)
	assert.False(t, exists, "source workitem directory should be gone")

	statusOutput, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(statusOutput), "git status should be clean after workitem dungeon move")

	diff := assertLastDungeonMoveCommit(t, tc, path, "Triage workitem demo-feature", "D\tworkflow/feature/demo-feature/.workitem")
	assert.Contains(t, diff, "workflow/feature/dungeon/OBEY.md")
	assert.Contains(t, diff, "workflow/feature/dungeon/archived/.gitkeep")
	assert.Regexp(t, `(?m)^A\tworkflow/feature/dungeon/archived/[0-9]{4}-[0-9]{2}-[0-9]{2}/demo-feature/.workitem$`, diff)
}

func TestDungeonMove_WorkitemByIDToLocalDungeonRootFromAnywhere(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-workitem-id")

	output, err := tc.RunCampInDir(path,
		"workitem", "create", "id-target",
		"--type", "bug",
		"--title", "ID target",
		"--id", "bug-id-target-fixed",
	)
	require.NoError(t, err, "workitem create should succeed: %s", output)
	_, _, err = tc.ExecCommand("sh", "-c", "mkdir -p "+path+"/docs && cd "+path+" && git add . && git commit -m 'add id workitem'")
	require.NoError(t, err)

	output, err = tc.RunCampInDir(path+"/docs", "dungeon", "move", "bug-id-target-fixed", "--workitem")
	require.NoError(t, err, "workitem dungeon root move should succeed: %s", output)
	assert.Contains(t, output, "Moved id-target")
	assert.Contains(t, output, "workflow/bug/dungeon/id-target")

	exists, err := tc.CheckDirExists(path + "/workflow/bug/dungeon/id-target")
	require.NoError(t, err)
	assert.True(t, exists, "workitem should move into its local dungeon root")

	exists, err = tc.CheckDirExists(path + "/workflow/bug/id-target")
	require.NoError(t, err)
	assert.False(t, exists, "source workitem directory should be gone")

	statusOutput, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(statusOutput), "git status should be clean after workitem dungeon root move")
}

func TestDungeonMove_WorkitemByRelativePath(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-workitem-path")

	output, err := tc.RunCampInDir(path,
		"workitem", "create", "path-target",
		"--type", "chore",
		"--title", "Path target",
	)
	require.NoError(t, err, "workitem create should succeed: %s", output)
	_, _, err = tc.ExecCommand("sh", "-c", "mkdir -p "+path+"/docs && cd "+path+" && git add . && git commit -m 'add path workitem'")
	require.NoError(t, err)

	output, err = tc.RunCampInDir(path+"/docs", "dungeon", "move", "workflow/chore/path-target", "archived", "--workitem")
	require.NoError(t, err, "workitem relative path move should succeed: %s", output)
	assert.Contains(t, output, "Moved path-target")
	assert.Contains(t, output, "workflow/chore/dungeon/archived")

	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/workflow/chore/dungeon/archived", "path-target")
	require.NoError(t, err)
	assert.True(t, exists, "workitem should move by relative path into its local archived dungeon")

	exists, err = tc.CheckDirExists(path + "/workflow/chore/path-target")
	require.NoError(t, err)
	assert.False(t, exists, "source workitem directory should be gone")
}
