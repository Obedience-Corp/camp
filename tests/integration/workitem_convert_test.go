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

func setupConvertWorkitem(t *testing.T, tc *TestContainer, name, wkType, slug string) string {
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

func TestWorkitemConvert_MovesTypeRootAndRepairsReferences(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupConvertWorkitem(t, tc, "wi-convert-refs", "explore", "camp-triage")

	require.NoError(t, tc.WriteFile(path+"/docs/note.md",
		"# Doc\n\nSee [triage](../workflow/explore/camp-triage/README.md).\n"))
	require.NoError(t, tc.WriteFile(path+"/workflow/explore/camp-triage/README.md", "# Camp Triage\n"))
	_, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'seed doc'")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(path, "workitem", "priority", "camp-triage", "high")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path, "workitem", "convert", "camp-triage", "--type", "design")
	require.NoError(t, err, "convert should succeed: %s", output)
	assert.Contains(t, output, "Converted workitem")

	exists, err := tc.CheckDirExists(path + "/workflow/design/camp-triage")
	require.NoError(t, err)
	assert.True(t, exists, "converted directory should exist under the new type root")
	exists, err = tc.CheckDirExists(path + "/workflow/explore/camp-triage")
	require.NoError(t, err)
	assert.False(t, exists, "old type-root directory should be gone")

	marker, err := tc.ReadFile(path + "/workflow/design/camp-triage/.workitem")
	require.NoError(t, err)
	assert.Contains(t, marker, "type: design")
	assert.NotContains(t, marker, "type: explore")
	assert.Contains(t, marker, "id: explore-camp-triage-fixed", "stable id must be preserved")

	note, err := tc.ReadFile(path + "/docs/note.md")
	require.NoError(t, err)
	assert.Contains(t, note, "../workflow/design/camp-triage/README.md")
	assert.NotContains(t, note, "explore/camp-triage/README.md")

	store, err := tc.ReadFile(path + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	assert.Contains(t, store, "design:workflow/design/camp-triage")
	assert.NotContains(t, store, "explore:workflow/explore/camp-triage\"")

	body := tc.GitOutput(t, path, "log", "-1", "--pretty=%B")
	assert.Contains(t, body, "Convert workitem camp-triage from explore to design")
	status, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(status), "git tree should be clean after convert")
}

func TestWorkitemConvert_LinksRegistryRepaired(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupConvertWorkitem(t, tc, "wi-convert-links", "explore", "alpha")

	require.NoError(t, tc.WriteFile(path+"/workflow/explore/alpha/sub/notes.md", "# Notes\n"))
	_, err := tc.RunCampInDir(path, "workitem", "link", "alpha", "workflow/explore/alpha/sub")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'seed link'")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(path, "workitem", "convert", "alpha", "--type", "design")
	require.NoError(t, err)

	registry, err := tc.ReadFile(path + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, registry, "workitem_key: design:workflow/design/alpha")
	assert.Contains(t, registry, "path: workflow/design/alpha/sub")
	assert.Contains(t, registry, "workitem_id: explore-alpha-fixed", "stable id must be preserved")
	assert.NotContains(t, registry, "explore/alpha")
}

func TestWorkitemConvert_JSON(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupConvertWorkitem(t, tc, "wi-convert-json", "explore", "widget")

	output, err := tc.RunCampInDir(path, "workitem", "convert", "widget", "--type", "design", "--json")
	require.NoError(t, err, "convert --json should succeed: %s", output)

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Workitem      struct {
			ID            string `json:"id"`
			Key           string `json:"key"`
			FromType      string `json:"from_type"`
			ToType        string `json:"to_type"`
			ItemKind      string `json:"item_kind"`
			From          string `json:"from"`
			To            string `json:"to"`
			Committed     bool   `json:"committed"`
			MarkerUpdated bool   `json:"marker_updated"`
		} `json:"workitem"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &payload), "output must be one JSON object: %s", output)
	assert.Equal(t, "workitem-convert/v1alpha1", payload.SchemaVersion)
	assert.Equal(t, "explore-widget-fixed", payload.Workitem.ID)
	assert.Equal(t, "design:workflow/design/widget", payload.Workitem.Key)
	assert.Equal(t, "explore", payload.Workitem.FromType)
	assert.Equal(t, "design", payload.Workitem.ToType)
	assert.Equal(t, "directory", payload.Workitem.ItemKind)
	assert.Equal(t, "workflow/explore/widget", payload.Workitem.From)
	assert.Equal(t, "workflow/design/widget", payload.Workitem.To)
	assert.True(t, payload.Workitem.Committed)
	assert.True(t, payload.Workitem.MarkerUpdated)
}

func TestWorkitemConvert_DryRun(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupConvertWorkitem(t, tc, "wi-convert-dry", "explore", "sprocket")

	head := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))
	output, err := tc.RunCampInDir(path, "workitem", "convert", "sprocket", "--type", "design", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, output, "dry-run")

	exists, err := tc.CheckDirExists(path + "/workflow/explore/sprocket")
	require.NoError(t, err)
	assert.True(t, exists, "dry-run must not move the workitem")
	assert.Equal(t, head, strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD")), "dry-run must not commit")
}

func TestWorkitemConvert_SameTypeRejected(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupConvertWorkitem(t, tc, "wi-convert-same", "design", "already")

	output, err := tc.RunCampInDir(path, "workitem", "convert", "already", "--type", "design")
	require.Error(t, err, "converting to the current type should fail")
	assert.Contains(t, output, "already type design")

	exists, err := tc.CheckDirExists(path + "/workflow/design/already")
	require.NoError(t, err)
	assert.True(t, exists, "source must be untouched after a rejected convert")
}

func TestWorkitemConvert_DestinationCollision(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupConvertWorkitem(t, tc, "wi-convert-collision", "explore", "shared")

	_, err := tc.RunCampInDir(path, "workitem", "create", "shared", "--type", "design", "--title", "shared", "--id", "design-shared-fixed")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path, "workitem", "convert", "explore-shared-fixed", "--type", "design")
	require.Error(t, err, "convert onto an existing destination should fail")
	assert.Contains(t, output, "already exists")

	exists, err := tc.CheckDirExists(path + "/workflow/explore/shared")
	require.NoError(t, err)
	assert.True(t, exists, "source must be untouched after a rejected convert")
}

func TestWorkitemConvert_FileWorkitem(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/wi-convert-file"
	_, err := tc.InitCampaign(path, "wi-convert-file", "product")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(path, "workitem", "create", "--file", "workflow/explore/note.md", "--type", "explore", "--title", "Note")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add file workitem'")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path, "workitem", "convert", "note.md", "--type", "design", "--json")
	require.NoError(t, err, "file convert should succeed: %s", output)

	var payload struct {
		Workitem struct {
			Key      string `json:"key"`
			ItemKind string `json:"item_kind"`
			To       string `json:"to"`
			ToType   string `json:"to_type"`
		} `json:"workitem"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &payload))
	assert.Equal(t, "file", payload.Workitem.ItemKind)
	assert.Equal(t, "workflow/design/note.md", payload.Workitem.To)
	assert.Equal(t, "file:workflow/design/note.md", payload.Workitem.Key)
	assert.Equal(t, "design", payload.Workitem.ToType)

	exists, err := tc.CheckFileExists(path + "/workflow/design/note.md")
	require.NoError(t, err)
	assert.True(t, exists, "converted file should exist under the new type root")

	body, err := tc.ReadFile(path + "/workflow/design/note.md")
	require.NoError(t, err)
	assert.Contains(t, body, "type: design")
	assert.NotContains(t, body, "type: explore")
}

func TestWorkitemConvert_RetypeAlias(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupConvertWorkitem(t, tc, "wi-convert-alias", "explore", "alias-item")

	output, err := tc.RunCampInDir(path, "workitem", "retype", "alias-item", "--type", "design")
	require.NoError(t, err, "retype alias should succeed: %s", output)
	assert.Contains(t, output, "Converted workitem")

	exists, err := tc.CheckDirExists(path + "/workflow/design/alias-item")
	require.NoError(t, err)
	assert.True(t, exists)
}
