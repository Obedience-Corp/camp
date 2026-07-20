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

// accItem parses the workitem --json item shape needed by the seq-01 acceptance
// gap-closing tests: tags and the merged {path, primary} projects view.
type accItem struct {
	Key          string   `json:"key"`
	RelativePath string   `json:"relative_path"`
	ItemKind     string   `json:"item_kind"`
	Tags         []string `json:"tags"`
	Projects     []struct {
		Path    string `json:"path"`
		Primary bool   `json:"primary"`
	} `json:"projects"`
}

func accItems(t *testing.T, tc *TestContainer, dir string, args ...string) []accItem {
	t.Helper()
	out, err := tc.RunCampInDir(dir, args...)
	require.NoError(t, err, "%v: %s", args, out)
	start := strings.Index(out, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON in: %s", out)
	var payload struct {
		Items []accItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(out[start:]), &payload), "raw=%s", out)
	return payload.Items
}

func accItemByPath(items []accItem, rel string) (accItem, bool) {
	for _, it := range items {
		if it.RelativePath == rel {
			return it, true
		}
	}
	return accItem{}, false
}

// doc 08 criterion 1: a directory marker with tags and a multi-entry projects
// list validates and appears in camp workitem --json with both fields populated.
func TestIntegration_Crit01_JSONShowsTagsAndMultiProject(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/acc-crit01"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "camp")
	seedProject(t, tc, dir, "fest")
	out, err := tc.RunCampInDir(dir, "workitem", "create", "feat", "--type", "design", "--title", "Feat",
		"--tag", "ux", "--tag", "public-launch",
		"--project", "projects/camp", "--project", "projects/fest")
	require.NoError(t, err, "create: %s", out)

	items := accItems(t, tc, dir, "workitem", "list", "--json")
	item, ok := accItemByPath(items, "workflow/design/feat")
	require.True(t, ok, "item not found in %+v", items)
	assert.Equal(t, []string{"ux", "public-launch"}, item.Tags, "both tags populated in order")
	require.Len(t, item.Projects, 2, "multi-entry projects populated")
	assert.Equal(t, "projects/camp", item.Projects[0].Path)
	assert.Equal(t, "projects/fest", item.Projects[1].Path)
}

// doc 08 criterion 13: a workitem whose marker fails schema validation is not
// silently included in a filtered result, and the validation failure surfaces
// (via validate) rather than being swallowed. Pinned behavior: the invalid
// marker discovers as a bare item (empty tags), so a tag filter excludes it,
// and validate reports it as an error finding.
func TestIntegration_Crit13_InvalidMarkerExcludedFromFilterAndSurfaced(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/acc-crit13"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "target")
	// A valid workitem carrying both the filter tag and the filter project.
	out, err := tc.RunCampInDir(dir, "workitem", "create", "good", "--type", "design", "--title", "Good",
		"--tag", "target", "--project", "projects/target")
	require.NoError(t, err, "create good: %s", out)
	// A directory marker with an invalid version (fails schema load) that also
	// claims tag "target" and project projects/target: if it loaded, it would
	// match both filters.
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/bad/.workitem",
		"version: v1alpha7x\nkind: workitem\nid: design-bad-2026-07-19\ntype: design\ntitle: Bad\nref: WI-bad001\ntags:\n  - target\nprojects:\n  - projects/target\n"))

	// The tag filter must not silently include the invalid marker.
	tagItems := accItems(t, tc, dir, "workitem", "list", "--tag", "target", "--json")
	_, goodInTag := accItemByPath(tagItems, "workflow/design/good")
	_, badInTag := accItemByPath(tagItems, "workflow/design/bad")
	assert.True(t, goodInTag, "the valid tagged item is present in the --tag result")
	assert.False(t, badInTag, "the invalid marker's tag claim does not leak into the --tag filter")

	// Symmetry: the project filter must not silently include it either.
	projItems := accItems(t, tc, dir, "workitem", "list", "--project", "projects/target", "--json")
	_, goodInProj := accItemByPath(projItems, "workflow/design/good")
	_, badInProj := accItemByPath(projItems, "workflow/design/bad")
	assert.True(t, goodInProj, "the valid item is present in the --project result")
	assert.False(t, badInProj, "the invalid marker's project claim does not leak into the --project filter")

	// The boundary is "excluded from filters", not "hidden": the invalid marker
	// still appears in an unfiltered list as a bare item, because its tags and
	// projects never loaded from the failed marker.
	allItems := accItems(t, tc, dir, "workitem", "list", "--json")
	bad, badPresent := accItemByPath(allItems, "workflow/design/bad")
	require.True(t, badPresent, "the invalid marker appears as a bare item in the unfiltered list")
	assert.Empty(t, bad.Tags, "the bare item exposes no loaded tags")
	assert.Empty(t, bad.Projects, "the bare item exposes no loaded projects")

	// The validation failure must surface via validate (non-zero exit + the path).
	vout, verr := tc.RunCampInDir(dir, "workitem", "validate", "--json")
	assert.Error(t, verr, "validate exits non-zero when a marker is malformed: %s", vout)
	assert.Contains(t, vout, "workflow/design/bad", "validate surfaces the invalid marker rather than swallowing it")
}

// doc 08 criterion 17 (the load-bearing one): a shipped pre-reader binary
// (v0.3.0-rc.2, commit 1f06e423, allowlist stops at v1alpha6, no forward-compat
// rule) hard-fails to load a v1alpha8 marker written by the current binary, with
// a non-zero exit and a versions error naming its supported list. This proves the
// staged reader-before-writer rollout was necessary.
func TestIntegration_Crit17_PreReaderBinaryRejectsV1Alpha8(t *testing.T) {
	if legacyCampSkip != "" {
		t.Skip(legacyCampSkip)
	}
	tc := GetSharedContainer(t)
	dir := "/test/acc-crit17"
	initLinksCampaign(t, tc, dir)

	// The CURRENT binary writes a v1alpha8 marker.
	out, err := tc.RunCampInDir(dir, "workitem", "create", "feat", "--type", "design", "--title", "Feat")
	require.NoError(t, err, "current create: %s", out)
	marker, err := tc.ReadFile(dir + "/workflow/design/feat/.workitem")
	require.NoError(t, err)
	require.Contains(t, marker, "version: v1alpha8", "the current binary writes a v1alpha8 marker")

	// The pre-reader binary must hard-fail on that campaign rather than silently
	// accept the marker: non-zero exit, versions error naming its supported list.
	lout, lerr := tc.RunLegacyCampInDir(dir, "workitem", "validate")
	require.Error(t, lerr, "pre-reader binary must exit non-zero on a v1alpha8 marker: %s", lout)
	assert.Contains(t, lout, "v1alpha8", "the error names the unsupported version it read")
	assert.Contains(t, lout, "v1alpha6", "the error names the pre-reader supported list (which stops at v1alpha6)")
}

// doc 08 criterion 9: an intent under .campaign/intents/<stage>/*.md is discovered
// only by the intents path, never a second time as a kind: workitem frontmatter
// document, because the document scanner does not descend into .campaign/.
func TestIntegration_Crit09_IntentNotDoubleDiscoveredAsFrontmatter(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/acc-crit09"
	initLinksCampaign(t, tc, dir)
	// The adversarial case: an intent file that also carries a kind: workitem
	// frontmatter block. If the document scanner descended into .campaign/, this
	// would be discovered a second time as a file workitem.
	require.NoError(t, tc.WriteFile(dir+"/.campaign/intents/active/01-idea.md",
		"---\nid: int_01\nstatus: active\nkind: workitem\ntype: design\nref: WI-aaaaaa\ntitle: Idea\n---\n\nbody\n"))

	items := accItems(t, tc, dir, "workitem", "list", "--json")
	rel := ".campaign/intents/active/01-idea.md"
	var matches []accItem
	for _, it := range items {
		if it.RelativePath == rel {
			matches = append(matches, it)
		}
	}
	require.Len(t, matches, 1, "the intent is discovered exactly once, never doubled as a frontmatter workitem: %+v", matches)
	assert.True(t, strings.HasPrefix(matches[0].Key, "intent:"),
		"the single discovery is via the intents path (key %q), not a frontmatter workitem", matches[0].Key)
}
