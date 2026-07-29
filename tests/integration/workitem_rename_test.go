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

// setupRenameWorkitem creates a campaign with one directory workitem of the
// given type and slug, committed, and returns the campaign path.
func setupRenameWorkitem(t *testing.T, tc *TestContainer, name, wkType, slug string) string {
	t.Helper()
	path := "/campaigns/" + name
	_, err := tc.InitCampaign(path, name, "product")
	require.NoError(t, err)

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

func TestWorkitemRename_RepairsReferences(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRenameWorkitem(t, tc, "wi-rename-refs", "design", "timeline")

	// An external doc that links at the workitem, plus a manual priority.
	require.NoError(t, tc.WriteFile(path+"/docs/note.md",
		"# Doc\n\nSee [timeline](../workflow/design/timeline/README.md).\n"))
	require.NoError(t, tc.WriteFile(path+"/workflow/design/timeline/README.md", "# Timeline\n"))
	_, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'seed doc'")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(path, "workitem", "priority", "timeline", "high")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path, "workitem", "rename", "timeline", "release-timeline")
	require.NoError(t, err, "rename should succeed: %s", output)
	assert.Contains(t, output, "Renamed workitem")

	// Directory moved.
	exists, err := tc.CheckDirExists(path + "/workflow/design/release-timeline")
	require.NoError(t, err)
	assert.True(t, exists, "renamed directory should exist")
	exists, err = tc.CheckDirExists(path + "/workflow/design/timeline")
	require.NoError(t, err)
	assert.False(t, exists, "old directory should be gone")

	// External markdown link rewritten.
	note, err := tc.ReadFile(path + "/docs/note.md")
	require.NoError(t, err)
	assert.Contains(t, note, "../workflow/design/release-timeline/README.md")
	assert.NotContains(t, note, "design/timeline/README.md")

	// Priority store re-keyed on disk (gitignored, not committed).
	store, err := tc.ReadFile(path + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	assert.Contains(t, store, "design:workflow/design/release-timeline")
	assert.NotContains(t, store, "design:workflow/design/timeline\"")

	// Auto-commit landed and the tree is clean.
	body := tc.GitOutput(t, path, "log", "-1", "--pretty=%B")
	assert.Contains(t, body, "Rename workitem timeline to release-timeline")
	status, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(status), "git tree should be clean after rename")
}

func TestWorkitemRename_LinksRegistryRepaired(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRenameWorkitem(t, tc, "wi-rename-links", "design", "alpha")

	require.NoError(t, tc.WriteFile(path+"/workflow/design/alpha/sub/notes.md", "# Notes\n"))
	_, err := tc.RunCampInDir(path, "workitem", "link", "alpha", "workflow/design/alpha/sub")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'seed link'")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(path, "workitem", "rename", "alpha", "beta")
	require.NoError(t, err)

	registry, err := tc.ReadFile(path + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, registry, "workitem_key: design:workflow/design/beta")
	assert.Contains(t, registry, "path: workflow/design/beta/sub")
	assert.Contains(t, registry, "workitem_id: design-alpha-fixed", "stable id must be preserved")
	assert.NotContains(t, registry, "design/alpha")
}

func TestWorkitemRename_JSON(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRenameWorkitem(t, tc, "wi-rename-json", "design", "widget")

	output, err := tc.RunCampInDir(path, "workitem", "rename", "widget", "gadget", "--json")
	require.NoError(t, err, "rename --json should succeed: %s", output)

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Workitem      struct {
			ID            string `json:"id"`
			Key           string `json:"key"`
			Type          string `json:"type"`
			ItemKind      string `json:"item_kind"`
			From          string `json:"from"`
			To            string `json:"to"`
			Committed     bool   `json:"committed"`
			PriorityMoved bool   `json:"priority_migrated"`
		} `json:"workitem"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &payload), "output must be one JSON object: %s", output)
	assert.Equal(t, "workitem-rename/v1alpha1", payload.SchemaVersion)
	assert.Equal(t, "design-widget-fixed", payload.Workitem.ID)
	assert.Equal(t, "design:workflow/design/gadget", payload.Workitem.Key)
	assert.Equal(t, "directory", payload.Workitem.ItemKind)
	assert.Equal(t, "workflow/design/widget", payload.Workitem.From)
	assert.Equal(t, "workflow/design/gadget", payload.Workitem.To)
	assert.True(t, payload.Workitem.Committed)
}

func TestWorkitemRename_DryRun(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRenameWorkitem(t, tc, "wi-rename-dry", "design", "sprocket")

	head := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))
	output, err := tc.RunCampInDir(path, "workitem", "rename", "sprocket", "cog", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, output, "dry-run")

	exists, err := tc.CheckDirExists(path + "/workflow/design/sprocket")
	require.NoError(t, err)
	assert.True(t, exists, "dry-run must not move the workitem")
	assert.Equal(t, head, strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD")), "dry-run must not commit")
}

func TestWorkitemRename_NoCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRenameWorkitem(t, tc, "wi-rename-nocommit", "design", "lever")

	head := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))
	output, err := tc.RunCampInDir(path, "workitem", "rename", "lever", "handle", "--no-commit")
	require.NoError(t, err, "rename --no-commit should succeed: %s", output)

	exists, err := tc.CheckDirExists(path + "/workflow/design/handle")
	require.NoError(t, err)
	assert.True(t, exists, "workitem should be moved on disk")
	assert.Equal(t, head, strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD")), "no new commit with --no-commit")

	status, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(status), "move should be visible in git status")
}

func TestWorkitemRename_SiblingCollision(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRenameWorkitem(t, tc, "wi-rename-collision", "design", "one")

	_, err := tc.RunCampInDir(path, "workitem", "create", "two", "--type", "design", "--title", "two", "--id", "design-two-fixed")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path, "workitem", "rename", "one", "two")
	require.Error(t, err, "rename onto an existing sibling should fail")
	assert.Contains(t, output, "already exists")

	exists, err := tc.CheckDirExists(path + "/workflow/design/one")
	require.NoError(t, err)
	assert.True(t, exists, "source must be untouched after a rejected rename")
}

func TestWorkitemRename_FileWorkitem(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/wi-rename-file"
	_, err := tc.InitCampaign(path, "wi-rename-file", "product")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(path, "workitem", "create", "--file", "workflow/explore/note.md", "--type", "explore", "--title", "Note")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add file workitem'")
	require.NoError(t, err)

	// Selector by full basename; the original .md extension is preserved.
	output, err := tc.RunCampInDir(path, "workitem", "rename", "note.md", "weekly-note", "--json")
	require.NoError(t, err, "file rename should succeed: %s", output)

	var payload struct {
		Workitem struct {
			Key      string `json:"key"`
			ItemKind string `json:"item_kind"`
			To       string `json:"to"`
		} `json:"workitem"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &payload))
	assert.Equal(t, "file", payload.Workitem.ItemKind)
	assert.Equal(t, "workflow/explore/weekly-note.md", payload.Workitem.To)
	assert.Equal(t, "file:workflow/explore/weekly-note.md", payload.Workitem.Key)

	exists, err := tc.CheckFileExists(path + "/workflow/explore/weekly-note.md")
	require.NoError(t, err)
	assert.True(t, exists, "renamed file should exist with preserved extension")
}
