//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPromoteWorkitem(t *testing.T, tc *TestContainer, name, wkType, slug string) string {
	t.Helper()
	path := setupDungeonCampaign(t, tc, name)

	output, err := tc.RunCampInDir(path,
		"workitem", "create", slug,
		"--type", wkType,
		"--title", slug,
		"--id", wkType+"-"+slug+"-fixed",
	)
	require.NoError(t, err, "workitem create should succeed: %s", output)

	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add workitem'")
	require.NoError(t, err, "initial commit should succeed")

	return path
}

func TestPromote_FromActive(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupPromoteWorkitem(t, tc, "promote-from-active", "design", "feature-x")

	wkDir := path + "/workflow/design/feature-x"
	output, err := tc.RunCampInDir(wkDir, "shelve", "completed")
	require.NoError(t, err, "shelve from active should succeed: %s", output)

	assert.Contains(t, output, "Shelved feature-x")
	assert.Contains(t, output, "workflow/design/feature-x")
	assert.Contains(t, output, "workflow/design/dungeon/completed")
	assert.Contains(t, output, "Committed", "should auto-commit")

	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/workflow/design/dungeon/completed", "feature-x")
	require.NoError(t, err)
	assert.True(t, exists, "workitem should be in dated dungeon/completed")

	exists, err = tc.CheckDirExists(path + "/workflow/design/feature-x")
	require.NoError(t, err)
	assert.False(t, exists, "source workitem directory should be gone")

	statusOutput, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(statusOutput), "git status should be clean after promote")
}

func TestPromote_FromSubdir(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupPromoteWorkitem(t, tc, "promote-from-subdir", "design", "feature-y")

	_, _, err := tc.ExecCommand("sh", "-c", "mkdir -p "+path+"/workflow/design/feature-y/notes/deep")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path+"/workflow/design/feature-y/notes/deep", "shelve", "archived")
	require.NoError(t, err, "shelve from deep subdir should succeed: %s", output)

	assert.Contains(t, output, "Shelved feature-y")
	assert.Contains(t, output, "workflow/design/dungeon/archived")

	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/workflow/design/dungeon/archived", "feature-y")
	require.NoError(t, err)
	assert.True(t, exists, "workitem should be in dated dungeon/archived")
}

func TestPromote_BetweenDungeonStatuses(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupPromoteWorkitem(t, tc, "promote-between-statuses", "design", "feature-z")

	_, err := tc.RunCampInDir(path+"/workflow/design/feature-z", "shelve", "completed")
	require.NoError(t, err)

	findOutput, _, err := tc.ExecCommand(
		"find",
		path+"/workflow/design/dungeon/completed",
		"-mindepth", "2", "-maxdepth", "2",
		"-name", "feature-z", "-type", "d",
	)
	require.NoError(t, err)
	currentDir := strings.TrimSpace(findOutput)
	require.NotEmpty(t, currentDir, "should have found dated dungeon dir for feature-z")

	output, err := tc.RunCampInDir(currentDir, "shelve", "archived")
	require.NoError(t, err, "shelve between dungeon statuses should succeed: %s", output)

	assert.Contains(t, output, "Shelved feature-z")
	assert.Contains(t, output, "workflow/design/dungeon/archived")

	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/workflow/design/dungeon/archived", "feature-z")
	require.NoError(t, err)
	assert.True(t, exists, "workitem should be in dated dungeon/archived after second promote")

	exists, err = tc.CheckDirExists(currentDir)
	require.NoError(t, err)
	assert.False(t, exists, "previous dungeon location should be empty")
}

func TestPromote_NotInWorkitem(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "promote-not-in-workitem")

	output, err := tc.RunCampInDir(path, "shelve", "completed")
	require.Error(t, err, "shelve outside workitem should fail")
	assert.Contains(t, output+err.Error(), "not inside a workitem")
}

func TestPromote_InvalidStatus(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupPromoteWorkitem(t, tc, "promote-invalid-status", "design", "feature-q")

	output, err := tc.RunCampInDir(path+"/workflow/design/feature-q", "shelve", "foo/bar")
	require.Error(t, err, "shelve with invalid status should fail")
	assert.Contains(t, output+err.Error(), "invalid status")
}

func TestPromote_AlreadyAtStatus(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupPromoteWorkitem(t, tc, "promote-already-at-status", "design", "feature-r")

	_, err := tc.RunCampInDir(path+"/workflow/design/feature-r", "shelve", "completed")
	require.NoError(t, err)

	findOutput, _, err := tc.ExecCommand(
		"find",
		path+"/workflow/design/dungeon/completed",
		"-mindepth", "2", "-maxdepth", "2",
		"-name", "feature-r", "-type", "d",
	)
	require.NoError(t, err)
	currentDir := strings.TrimSpace(findOutput)
	require.NotEmpty(t, currentDir)

	output, err := tc.RunCampInDir(currentDir, "shelve", "completed")
	require.Error(t, err, "re-shelve to same status should fail")
	assert.Contains(t, output+err.Error(), "already at status")
}

func TestPromote_ActiveRejected(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupPromoteWorkitem(t, tc, "promote-active-rejected", "design", "feature-a")

	output, err := tc.RunCampInDir(path+"/workflow/design/feature-a", "shelve", "active")
	require.Error(t, err, "shelve active should fail")
	assert.Contains(t, output+err.Error(), "not a dungeon status")

	exists, err := tc.CheckDirExists(path + "/workflow/design/feature-a")
	require.NoError(t, err)
	assert.True(t, exists, "workitem should remain in active location")

	exists, err = tc.CheckDirExists(path + "/workflow/design/dungeon/active")
	require.NoError(t, err)
	assert.False(t, exists, "no dungeon/active directory should be created")
}

func TestPromote_JsonOutput(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupPromoteWorkitem(t, tc, "promote-json", "design", "feature-j")

	output, err := tc.RunCampInDir(path+"/workflow/design/feature-j", "shelve", "completed", "--json")
	require.NoError(t, err, "shelve --json should succeed: %s", output)

	trimmed := strings.TrimSpace(output)
	require.NotEmpty(t, trimmed, "JSON output should not be empty")

	var result struct {
		Slug          string   `json:"slug"`
		Type          string   `json:"type"`
		Status        string   `json:"status"`
		From          string   `json:"from"`
		To            string   `json:"to"`
		Committed     bool     `json:"committed"`
		CommitMessage string   `json:"commit_message"`
		Warnings      []string `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal([]byte(trimmed), &result), "JSON output must be a single parseable object: %s", output)

	assert.Equal(t, "feature-j", result.Slug)
	assert.Equal(t, "design", result.Type)
	assert.Equal(t, "completed", result.Status)
	assert.Equal(t, "workflow/design/feature-j", result.From)
	assert.Contains(t, result.To, "workflow/design/dungeon/completed/")
	assert.True(t, result.Committed, "commit should have succeeded")
	assert.NotEmpty(t, result.CommitMessage, "commit message should be set")
}

func TestPromote_JsonOutputNoCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupPromoteWorkitem(t, tc, "promote-json-no-commit", "design", "feature-k")

	output, err := tc.RunCampInDir(path+"/workflow/design/feature-k", "shelve", "someday", "--json", "--no-commit")
	require.NoError(t, err, "shelve --json --no-commit should succeed: %s", output)

	var result struct {
		Committed bool `json:"committed"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &result), "JSON output must parse: %s", output)
	assert.False(t, result.Committed, "committed should be false with --no-commit")
}

func TestPromote_NoCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupPromoteWorkitem(t, tc, "promote-no-commit", "design", "feature-s")

	headBefore := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))

	output, err := tc.RunCampInDir(path+"/workflow/design/feature-s", "shelve", "someday", "--no-commit")
	require.NoError(t, err, "shelve --no-commit should succeed: %s", output)

	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/workflow/design/dungeon/someday", "feature-s")
	require.NoError(t, err)
	assert.True(t, exists, "workitem should still be moved on disk")

	headAfter := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))
	assert.Equal(t, headBefore, headAfter, "no new commit should be created with --no-commit")

	statusOutput, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(statusOutput), "filesystem move should appear in git status")
}
