//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDungeonMove_TriageToDocsDestination(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-triage-docs")

	_, _, err := tc.ExecCommand("mkdir", "-p", path+"/docs/architecture/api")
	require.NoError(t, err)

	// Create item in parent directory.
	err = tc.WriteFile(path+"/legacy-notes.md", "# Legacy Notes\n")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add legacy-notes.md && git commit -m 'add legacy notes'")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(
		path,
		"dungeon", "move", "legacy-notes.md",
		"--triage",
		"--to-docs", "architecture/api",
	)
	require.NoError(t, err)
	assert.Contains(t, output, "Moved legacy-notes.md", "should confirm docs routing move")
	assert.Contains(t, output, "docs/architecture/api/legacy-notes.md", "should show docs destination")
	assert.Contains(t, output, "Committed", "should auto-commit docs routing move")

	exists, err := tc.CheckFileExists(path + "/docs/architecture/api/legacy-notes.md")
	require.NoError(t, err)
	assert.True(t, exists, "file should be routed into campaign-root docs destination")

	exists, err = tc.CheckFileExists(path + "/legacy-notes.md")
	require.NoError(t, err)
	assert.False(t, exists, "file should no longer be in parent")

	assertLastDungeonMoveCommit(
		t,
		tc,
		path,
		"Route legacy-notes.md",
		"D\tlegacy-notes.md",
		"A\tdocs/architecture/api/legacy-notes.md",
	)
}

func TestDungeonMove_TriageToDocsRequiresExistingSubdirectory(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-triage-docs-missing-subdir")

	err := tc.WriteFile(path+"/legacy-notes.md", "# Legacy Notes\n")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(
		path,
		"dungeon", "move", "legacy-notes.md",
		"--triage",
		"--to-docs", "architecture/api",
	)
	assert.Error(t, err)
	assert.Contains(t, output, "invalid docs destination", "error should explain destination rules")
	assert.Contains(t, output, "does not exist under campaign-root docs", "error should require existing docs subdirectory")

	exists, statErr := tc.CheckFileExists(path + "/legacy-notes.md")
	require.NoError(t, statErr)
	assert.True(t, exists, "file should remain in parent after failed docs routing")
}

func TestDungeonMove_ToDocsRequiresTriage(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-docs-needs-triage")

	output, err := tc.RunCampInDir(
		path,
		"dungeon", "move", "anything.md",
		"--to-docs", "architecture/api",
	)
	assert.Error(t, err)
	assert.Contains(t, output, "--to-docs requires --triage")
}

func TestDungeonMove_TriageToDocsRejectsTraversal(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-triage-docs-invalid")

	err := tc.WriteFile(path+"/legacy-notes.md", "# Legacy Notes\n")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(
		path,
		"dungeon", "move", "legacy-notes.md",
		"--triage",
		"--to-docs", "../escape",
	)
	assert.Error(t, err)
	assert.Contains(t, output, "invalid docs destination", "error should explain destination rules")

	exists, statErr := tc.CheckFileExists(path + "/legacy-notes.md")
	require.NoError(t, statErr)
	assert.True(t, exists, "file should remain in parent after failed docs routing")
}

func TestDungeonMove_TriageToDocsRejectsTraversalItemName(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-triage-docs-item-traversal")

	// Add nested dungeon context and nested execution path.
	_, _, err := tc.ExecCommand(
		"mkdir", "-p",
		path+"/workflow/design/dungeon",
		path+"/workflow/design/subdir",
		path+"/docs/architecture",
	)
	require.NoError(t, err)

	// File outside nearest parent context that traversal would previously target.
	err = tc.WriteFile(path+"/workflow/secret.md", "# Secret\n")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(
		path+"/workflow/design/subdir",
		"dungeon", "move", "../secret.md",
		"--triage",
		"--to-docs", "architecture",
	)
	assert.Error(t, err)
	assert.Contains(t, output, "invalid item path")

	exists, statErr := tc.CheckFileExists(path + "/workflow/secret.md")
	require.NoError(t, statErr)
	assert.True(t, exists, "source file should remain in original location")

	exists, statErr = tc.CheckFileExists(path + "/docs/secret.md")
	require.NoError(t, statErr)
	assert.False(t, exists, "docs-root bypass target should not be created")

	exists, statErr = tc.CheckFileExists(path + "/docs/architecture/secret.md")
	require.NoError(t, statErr)
	assert.False(t, exists, "selected docs destination should not receive invalid path item")
}

func TestDungeonMove_TriageRejectsNestedItemPath(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-triage-invalid-item-path")

	_, _, err := tc.ExecCommand("mkdir", "-p", path+"/nested")
	require.NoError(t, err)
	err = tc.WriteFile(path+"/nested/legacy.md", "# Legacy\n")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(
		path,
		"dungeon", "move", "nested/legacy.md",
		"--triage",
	)
	assert.Error(t, err)
	assert.Contains(t, output, "invalid item path")

	exists, statErr := tc.CheckFileExists(path + "/nested/legacy.md")
	require.NoError(t, statErr)
	assert.True(t, exists, "nested source file should remain in place")

	exists, statErr = tc.CheckFileExists(path + "/dungeon/nested/legacy.md")
	require.NoError(t, statErr)
	assert.False(t, exists, "invalid nested item path should not be moved into dungeon")
}

func TestDungeonMove_TriageToDocsFromNestedDirAnchorsToCampaignRootDocs(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-docs-root-anchor")

	// Add nested dungeon context and nested execution path.
	_, _, err := tc.ExecCommand(
		"mkdir", "-p",
		path+"/workflow/design/dungeon",
		path+"/workflow/design/subdir",
		path+"/docs/architecture/reference",
		path+"/workflow/design/docs/architecture/reference",
	)
	require.NoError(t, err)

	// Parent item for nearest nested dungeon context.
	err = tc.WriteFile(path+"/workflow/design/legacy-spec.md", "# Legacy Spec\n")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(
		path+"/workflow/design/subdir",
		"dungeon", "move", "legacy-spec.md",
		"--triage",
		"--to-docs", "architecture/reference",
	)
	require.NoError(t, err)
	assert.Contains(t, output, "docs/architecture/reference/legacy-spec.md")

	exists, err := tc.CheckFileExists(path + "/docs/architecture/reference/legacy-spec.md")
	require.NoError(t, err)
	assert.True(t, exists, "file should route to campaign-root docs/")

	exists, err = tc.CheckFileExists(path + "/workflow/design/docs/architecture/reference/legacy-spec.md")
	require.NoError(t, err)
	assert.False(t, exists, "file should not route to dungeon-local docs path")
}

func TestDungeonMove_TriageFromNestedDirUsesNearestContext(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "dmove-nearest")

	// Create nearest dungeon context and nested execution path.
	_, _, err := tc.ExecCommand("mkdir", "-p", path+"/workflow/design/dungeon", path+"/workflow/design/subdir")
	require.NoError(t, err)

	// Parent item for nearest context lives in workflow/design.
	err = tc.WriteFile(path+"/workflow/design/nested-old.md", "# Nested Old\n")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path+"/workflow/design/subdir", "dungeon", "move", "nested-old.md", "--triage")
	require.NoError(t, err)
	assert.Contains(t, output, "Moved nested-old.md", "should confirm move from nested context")

	exists, err := tc.CheckFileExists(path + "/workflow/design/dungeon/nested-old.md")
	require.NoError(t, err)
	assert.True(t, exists, "file should move into nearest dungeon context")
}
