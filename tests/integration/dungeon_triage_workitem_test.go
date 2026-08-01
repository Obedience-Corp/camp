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

type triageItem struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	WorkitemType string `json:"workitem_type"`
}

func triageJSON(t *testing.T, tc *TestContainer, dir string) map[string]triageItem {
	t.Helper()
	out, err := tc.RunCampInDir(dir, "dungeon", "list", "--triage", "--json")
	require.NoError(t, err, "triage list: %s", out)

	start := strings.Index(out, "[")
	require.GreaterOrEqual(t, start, 0, "no JSON array in output: %s", out)
	var items []triageItem
	require.NoError(t, json.Unmarshal([]byte(out[start:]), &items), "must parse: %s", out)

	byName := make(map[string]triageItem, len(items))
	for _, it := range items {
		byName[it.Name] = it
	}
	return byName
}

func writeWorkitemDir(t *testing.T, tc *TestContainer, dir, wfType, slug string) {
	t.Helper()
	body := "version: v1alpha8\nkind: workitem\nid: " + wfType + "-" + slug + "-1\n" +
		"type: " + wfType + "\ntitle: " + slug + "\nref: WI-aaaaaa\n"
	require.NoError(t, tc.WriteFile(dir+"/.workitem", body))
	require.NoError(t, tc.WriteFile(dir+"/README.md", "# "+slug+"\n"))
}

// Triage never reaches festivals/. The excluder lists "festivals" by name
// (internal/dungeon/service_list.go), and `camp dungeon add` from inside festivals/
// resolves the campaign-root dungeon rather than creating one there, so the parent
// under triage is the campaign root, where festivals/ is excluded outright.
//
// This extends the discrepancy recorded in task 02: doc 04 asks for resident
// awareness "in the inner crawl of festivals/.dungeon/", and neither that crawl nor
// a triage view over festivals/ exists. Type-local triage is the one real surface,
// covered by the next test.
func TestDungeonTriage_FestivalsIsNotATriageParent(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "triage-wi-festivals")

	writeWorkitemDir(t, tc, path+"/festivals/marked", "design", "marked")
	require.NoError(t, tc.WriteFile(path+"/festivals/plaindir/NOTES.md", "# nothing\n"))

	out, err := tc.RunCampInDir(path+"/festivals", "dungeon", "list", "--triage")
	require.NoError(t, err, "triage list: %s", out)
	assert.Contains(t, out, "No parent items eligible for triage",
		"festivals/ is excluded from triage, so nothing under it is a candidate")

	got := triageJSON(t, tc, path+"/festivals")
	assert.Empty(t, got, "no triage candidates resolve from festivals/: %+v", got)
}

// The same field appears in a type-local triage context, because ListParentItems is
// shared. Recorded as a deliberate consequence rather than special-cased by path.
func TestDungeonTriage_WorkitemTypeInTypeLocalParent(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "triage-wi-typelocal")

	out, err := tc.RunCampInDir(path, "workitem", "create", "real-design",
		"--type", "design", "--title", "Real Design", "--id", "design-real-design-fixed")
	require.NoError(t, err, "workitem create: %s", out)
	require.NoError(t, tc.WriteFile(path+"/workflow/design/plaindir/NOTES.md", "# nothing\n"))

	got := triageJSON(t, tc, path+"/workflow/design")

	item, ok := got["real-design"]
	require.True(t, ok, "design workitem missing: %+v", got)
	assert.Equal(t, "design", item.WorkitemType)

	plain, ok := got["plaindir"]
	require.True(t, ok, "plain directory missing: %+v", got)
	assert.Empty(t, plain.WorkitemType)
}

// A file must never carry the field, and its row is otherwise unchanged.
func TestDungeonTriage_FilesUnaffected(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "triage-wi-files")

	require.NoError(t, tc.WriteFile(path+"/workflow/design/loose.md", "# loose\n"))

	got := triageJSON(t, tc, path+"/workflow/design")
	loose, ok := got["loose.md"]
	require.True(t, ok, "loose file missing: %+v", got)
	assert.Equal(t, "file", loose.Type)
	assert.Empty(t, loose.WorkitemType)
}
