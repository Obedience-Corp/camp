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

type mergedViewProjectRef struct {
	Path    string `json:"path"`
	Primary bool   `json:"primary"`
}

type mergedViewItem struct {
	RelativePath string                 `json:"relative_path"`
	Projects     []mergedViewProjectRef `json:"projects"`
}

// mergedViewItems runs a camp workitem JSON command and returns the parsed
// items. args is the full camp invocation (e.g. "workitem", "list", "--json"),
// letting one helper cover both --json call sites (list.go and workitem.go).
func mergedViewItems(t *testing.T, tc *TestContainer, dir string, args ...string) (items []mergedViewItem, raw string) {
	t.Helper()
	out, err := tc.RunCampInDir(dir, args...)
	require.NoError(t, err, "%v: %s", args, out)
	start := strings.Index(out, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON in: %s", out)
	var payload struct {
		Items []mergedViewItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(out[start:]), &payload), "raw=%s", out)
	return payload.Items, out
}

func mergedViewByPath(items []mergedViewItem, relPath string) (mergedViewItem, bool) {
	for _, it := range items {
		if it.RelativePath == relPath {
			return it, true
		}
	}
	return mergedViewItem{}, false
}

func TestIntegration_MergedProjectsViewPrimaryAnnotation(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/merged-view-primary"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "camp")
	seedProject(t, tc, dir, "fest")

	// Base projects: list on the marker.
	out, err := tc.RunCampInDir(dir, "workitem", "create", "wa", "--type", "design", "--title", "wa",
		"--project", "projects/camp", "--project", "projects/fest")
	require.NoError(t, err, "create: %s", out)
	// Primary link for projects/camp only. The link command's --project takes a
	// bare project name (it prefixes projects/), unlike create's full path.
	out, err = tc.RunCampInDir(dir, "workitem", "link", "wa", "--project", "camp")
	require.NoError(t, err, "link primary: %s", out)

	// Both --json call sites must produce the same merged, primary-annotated view.
	for _, cmd := range [][]string{
		{"workitem", "list", "--json"},
		{"workitem", "--json"},
	} {
		items, _ := mergedViewItems(t, tc, dir, cmd...)
		item, ok := mergedViewByPath(items, "workflow/design/wa")
		require.True(t, ok, "%v: item not found in %+v", cmd, items)
		assert.Equal(t, []mergedViewProjectRef{
			{Path: "projects/camp", Primary: true},
			{Path: "projects/fest", Primary: false},
		}, item.Projects, "%v: merged view must annotate the primary project", cmd)
	}
}

func TestIntegration_MergedProjectsViewMalformedRegistryFallback(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/merged-view-malformed"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "camp")
	out, err := tc.RunCampInDir(dir, "workitem", "create", "wa", "--type", "design", "--title", "wa",
		"--project", "projects/camp")
	require.NoError(t, err, "create: %s", out)
	// A links.yaml with no version field is rejected by links.Load. The read-only
	// listing must not hard-fail on it: it degrades to no primary annotation and
	// warns, rather than returning an error envelope.
	require.NoError(t, tc.WriteFile(dir+"/.campaign/workitems/links.yaml", "links: []\n"))

	// Both --json call sites must degrade gracefully.
	for _, cmd := range [][]string{
		{"workitem", "list", "--json"},
		{"workitem", "--json"},
	} {
		items, raw := mergedViewItems(t, tc, dir, cmd...)
		assert.Contains(t, raw, "camp workitem doctor --fix",
			"%v: the fallback warning must name the fix command", cmd)
		item, ok := mergedViewByPath(items, "workflow/design/wa")
		require.True(t, ok, "%v: item not found in %+v", cmd, items)
		require.Len(t, item.Projects, 1, "%v: projects must still be present", cmd)
		assert.Equal(t, "projects/camp", item.Projects[0].Path)
		assert.False(t, item.Projects[0].Primary,
			"%v: no primary annotation is available when the registry is unreadable", cmd)
	}
}

func TestIntegration_MergedProjectsViewEmptyNeverNull(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/merged-view-empty"
	initLinksCampaign(t, tc, dir)
	seedDesignWorkitem(t, tc, dir, "noproj")

	items, raw := mergedViewItems(t, tc, dir, "workitem", "list", "--json")
	item, ok := mergedViewByPath(items, "workflow/design/noproj")
	require.True(t, ok, "item not found in %+v", items)
	require.NotNil(t, item.Projects, "projects must be [] not null when there are none")
	assert.Len(t, item.Projects, 0)
	// Guard against a null literal slipping through the encoder for this item.
	assert.NotContains(t, raw, `"projects": null`, "the merged view must never serialize null")
}
