//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func checkDatedDungeonStatusItemExists(tc *TestContainer, statusPath, itemName string) (bool, error) {
	output, _, err := tc.ExecCommand(
		"find",
		statusPath,
		"-mindepth", "2",
		"-maxdepth", "2",
		"-name", itemName,
		"-print",
		"-quit",
	)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}
func assertLastDungeonMoveCommit(t *testing.T, tc *TestContainer, repoPath, wantBody string, wantNameStatus ...string) string {
	t.Helper()

	subject := tc.GitOutput(t, repoPath, "log", "-1", "--pretty=%s")
	assert.Contains(t, subject, "Crawl: dungeon crawl completed", "dungeon move should create a crawl commit")

	body := tc.GitOutput(t, repoPath, "log", "-1", "--pretty=%B")
	if wantBody != "" {
		assert.Contains(t, body, wantBody, "crawl commit body should describe the move")
	}

	diff := tc.GitOutput(t, repoPath, "diff-tree", "--no-commit-id", "--name-status", "--no-renames", "-r", "HEAD")
	for _, want := range wantNameStatus {
		assert.Contains(t, diff, want, "crawl commit should include expected name-status entry")
	}
	return diff
}
func TestDungeonMove_ToStatus(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-status")

	// Create item in dungeon root
	err := tc.WriteFile(path+"/dungeon/old-feature.md", "# Old Feature\n")
	require.NoError(t, err)

	// Git commit first so move has something to commit
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add dungeon item'")
	require.NoError(t, err)

	// Move to archived
	output, err := tc.RunCampInDir(path, "dungeon", "move", "old-feature.md", "archived")
	require.NoError(t, err)
	assert.Contains(t, output, "Moved old-feature.md", "should confirm move")
	assert.Contains(t, output, "archived", "should mention target status")

	// Verify file moved
	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/dungeon/archived", "old-feature.md")
	require.NoError(t, err)
	assert.True(t, exists, "file should be in archived/")

	// Verify file removed from root
	exists, err = tc.CheckFileExists(path + "/dungeon/old-feature.md")
	require.NoError(t, err)
	assert.False(t, exists, "file should no longer be in dungeon root")

	diff := assertLastDungeonMoveCommit(
		t,
		tc,
		path,
		"Moved to dungeon/archived:",
		"D\tdungeon/old-feature.md",
	)
	assert.Regexp(t, `(?m)^A\tdungeon/archived/[0-9]{4}-[0-9]{2}-[0-9]{2}/old-feature\.md$`, diff)
}
func TestDungeonMove_ToCompleted(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-comp")

	err := tc.WriteFile(path+"/dungeon/done-work.md", "# Done Work\n")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path, "dungeon", "move", "done-work.md", "completed")
	require.NoError(t, err)
	assert.Contains(t, output, "completed")

	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/dungeon/completed", "done-work.md")
	require.NoError(t, err)
	assert.True(t, exists, "file should be in completed/")
}
func TestDungeonMove_ToSomeday(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-some")

	err := tc.WriteFile(path+"/dungeon/maybe-later.md", "# Maybe Later\n")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path, "dungeon", "move", "maybe-later.md", "someday")
	require.NoError(t, err)
	assert.Contains(t, output, "someday")

	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/dungeon/someday", "maybe-later.md")
	require.NoError(t, err)
	assert.True(t, exists, "file should be in someday/")
}
func TestDungeonMove_MissingStatusError(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-nostat")

	err := tc.WriteFile(path+"/dungeon/item.md", "# Item\n")
	require.NoError(t, err)

	// Move without status (and without --triage) should fail
	output, err := tc.RunCampInDir(path, "dungeon", "move", "item.md")
	assert.Error(t, err, "should fail without status")
	assert.Contains(t, output, "status is required", "error should explain what's missing")
}
func TestDungeonMove_NotFoundError(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-notfound")

	output, err := tc.RunCampInDir(path, "dungeon", "move", "nonexistent.md", "archived")
	assert.Error(t, err, "should fail for nonexistent item")
	assert.Contains(t, output, "not found", "error should mention item not found")
}
func TestDungeonMove_TriageToDungeonRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-triage")

	// Create item in parent directory
	err := tc.WriteFile(path+"/old-project.md", "# Old Project\n")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path, "dungeon", "move", "old-project.md", "--triage")
	require.NoError(t, err)
	assert.Contains(t, output, "Moved old-project.md", "should confirm triage")

	// Verify moved to dungeon root
	exists, err := tc.CheckFileExists(path + "/dungeon/old-project.md")
	require.NoError(t, err)
	assert.True(t, exists, "file should be in dungeon/")

	// Verify removed from parent
	exists, err = tc.CheckFileExists(path + "/old-project.md")
	require.NoError(t, err)
	assert.False(t, exists, "file should no longer be in parent")
}
func TestDungeonMove_TriageDirectToStatus(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-triage-status")

	// Create item in parent directory
	err := tc.WriteFile(path+"/legacy-code.md", "# Legacy Code\n")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path, "dungeon", "move", "legacy-code.md", "archived", "--triage")
	require.NoError(t, err)
	assert.Contains(t, output, "archived", "should mention target status")

	// Verify moved directly to archived
	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/dungeon/archived", "legacy-code.md")
	require.NoError(t, err)
	assert.True(t, exists, "file should be in dungeon/archived/")

	// Verify not in parent or dungeon root
	exists, err = tc.CheckFileExists(path + "/legacy-code.md")
	require.NoError(t, err)
	assert.False(t, exists, "file should not be in parent")

	exists, err = tc.CheckFileExists(path + "/dungeon/legacy-code.md")
	require.NoError(t, err)
	assert.False(t, exists, "file should not be in dungeon root")
}
func TestDungeonMove_TriageWithCommit_IncludesSourceDeletion(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-triage-commit")

	// Create and commit a tracked file in the parent directory
	err := tc.WriteFile(path+"/tracked-item.md", "# Tracked Item\nContent here")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add tracked item'")
	require.NoError(t, err)

	// Triage move WITHOUT --no-commit — exercises commit.Crawl with source deletion
	output, err := tc.RunCampInDir(path, "dungeon", "move", "tracked-item.md", "--triage")
	require.NoError(t, err)
	assert.Contains(t, output, "Moved tracked-item.md", "should confirm triage")
	assert.Contains(t, output, "Committed", "should auto-commit")

	// Verify file moved to dungeon
	exists, err := tc.CheckFileExists(path + "/dungeon/tracked-item.md")
	require.NoError(t, err)
	assert.True(t, exists, "file should be in dungeon/")

	// Verify source removed
	exists, err = tc.CheckFileExists(path + "/tracked-item.md")
	require.NoError(t, err)
	assert.False(t, exists, "file should no longer be in parent")

	assertLastDungeonMoveCommit(
		t,
		tc,
		path,
		"Triage tracked-item.md",
		"D\ttracked-item.md",
		"A\tdungeon/tracked-item.md",
	)
}
func TestDungeonMove_TriageStagingFailureWarnsMoveApplied(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-triage-stage-fail")

	err := tc.WriteFile(path+"/tracked-broken.md", "# Tracked Broken\n")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add tracked broken'")
	require.NoError(t, err)

	output, exitCode, err := tc.ExecCommand(
		"sh",
		"-c",
		"cd "+path+" && PATH=/tmp/no-git /camp dungeon move tracked-broken.md --triage 2>&1",
	)
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "missing git should make pre-staging fail")
	assert.Contains(t, output, "Moved tracked-broken.md", "move should happen before staging failure")
	assert.Contains(t, output, "Move was applied on disk, but staging the source deletion failed.")
	assert.Contains(t, output, "staging move source deletions")

	exists, err := tc.CheckFileExists(path + "/dungeon/tracked-broken.md")
	require.NoError(t, err)
	assert.True(t, exists, "destination should remain after staging failure")

	exists, err = tc.CheckFileExists(path + "/tracked-broken.md")
	require.NoError(t, err)
	assert.False(t, exists, "source should already be moved after staging failure")
}
func TestDungeonMove_TriageDirectToStatusWithCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-triage-status-commit")

	// Create and commit a tracked file
	err := tc.WriteFile(path+"/stale-doc.md", "# Stale Doc\nOutdated content")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add stale doc'")
	require.NoError(t, err)

	// Triage directly to archived status WITHOUT --no-commit
	output, err := tc.RunCampInDir(path, "dungeon", "move", "stale-doc.md", "archived", "--triage")
	require.NoError(t, err)
	assert.Contains(t, output, "archived", "should mention target status")
	assert.Contains(t, output, "Committed", "should auto-commit")

	// Verify file at final destination
	exists, err := checkDatedDungeonStatusItemExists(tc, path+"/dungeon/archived", "stale-doc.md")
	require.NoError(t, err)
	assert.True(t, exists, "file should be in dungeon/archived/")

	// Verify source is gone
	exists, err = tc.CheckFileExists(path + "/stale-doc.md")
	require.NoError(t, err)
	assert.False(t, exists, "file should no longer be in parent")

	// Verify clean git status (no unstaged changes left behind)
	statusOutput, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(statusOutput), "git status should be clean after commit")

	diff := assertLastDungeonMoveCommit(
		t,
		tc,
		path,
		"Triage stale-doc.md",
		"D\tstale-doc.md",
	)
	assert.Regexp(t, `(?m)^A\tdungeon/archived/[0-9]{4}-[0-9]{2}-[0-9]{2}/stale-doc\.md$`, diff)
}
