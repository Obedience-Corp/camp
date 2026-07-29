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

type fmListItem struct {
	ItemKind     string `json:"item_kind"`
	WorkflowType string `json:"workflow_type"`
	RelativePath string `json:"relative_path"`
	StableID     string `json:"stable_id"`
}

func fmListItems(t *testing.T, tc *TestContainer, dir string) []fmListItem {
	t.Helper()
	out, err := tc.RunCampInDir(dir, "workitem", "list", "--json")
	require.NoError(t, err, "list --json: %s", out)
	start := strings.Index(out, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON in: %s", out)
	var payload struct {
		Items []fmListItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(out[start:]), &payload))
	return payload.Items
}

func fmItemByPath(items []fmListItem, relPath string) (fmListItem, bool) {
	for _, i := range items {
		if i.RelativePath == relPath {
			return i, true
		}
	}
	return fmListItem{}, false
}

func fmDoc(id, typ, title, ref string) string {
	return "---\nversion: v1alpha8\nkind: workitem\nid: " + id +
		"\ntype: " + typ + "\ntitle: " + title + "\nref: " + ref + "\n---\n\n# " + title + "\n"
}

func fmMarker(id, typ, ref string) string {
	return "version: v1alpha8\nkind: workitem\nid: " + id + "\ntype: " + typ + "\ntitle: " + id + "\nref: " + ref + "\n"
}

func fmValidateFindings(t *testing.T, tc *TestContainer, dir string) (codes map[string]string, nonZero bool) {
	t.Helper()
	out, err := tc.RunCampInDir(dir, "workitem", "validate", "--json")
	nonZero = err != nil
	start := strings.Index(out, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON in validate output: %s", out)
	var payload struct {
		Findings []struct {
			Code     string `json:"code"`
			Severity string `json:"severity"`
			Target   string `json:"target"`
		} `json:"findings"`
	}
	require.NoError(t, json.Unmarshal([]byte(out[start:]), &payload))
	codes = make(map[string]string)
	for _, f := range payload.Findings {
		codes[f.Code+"|"+f.Target] = f.Severity
	}
	return codes, nonZero
}

func TestIntegration_FrontmatterDiscoveryAndCollision(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/fm-collision"
	_, err := tc.InitCampaign(dir, "fm-collision", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/notes/doc.md", fmDoc("note-doc-2026-07-19", "chore", "Doc", "WI-aaaaaa")))
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/pkg/.workitem", fmMarker("design-pkg-2026-07-19", "design", "WI-bbbbbb")))
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/pkg/child.md", fmDoc("note-child-2026-07-19", "design", "Child", "WI-cccccc")))
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/pkg/sub/deep.md", fmDoc("note-deep-2026-07-19", "design", "Deep", "WI-dddddd")))
	// sibling of the marker dir, no marker of its own: hasMarkerAncestor ascends
	// only, so this file must still be discovered.
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/other/note.md", fmDoc("note-sibling-2026-07-19", "design", "Sibling", "WI-eeeeee")))

	items := fmListItems(t, tc, dir)

	doc, ok := fmItemByPath(items, "workflow/notes/doc.md")
	require.True(t, ok, "standalone frontmatter doc must be discovered")
	assert.Equal(t, "file", doc.ItemKind)

	_, childFound := fmItemByPath(items, "workflow/design/pkg/child.md")
	assert.False(t, childFound, "collision: immediate child of a marker dir must not be discovered")

	_, deepFound := fmItemByPath(items, "workflow/design/pkg/sub/deep.md")
	assert.False(t, deepFound, "collision: a file two levels below a marker-bearing ancestor must not be discovered")

	sib, sibFound := fmItemByPath(items, "workflow/design/other/note.md")
	assert.True(t, sibFound, "a file in a sibling directory of a marker dir must still be discovered (ascent-only)")
	assert.Equal(t, "file", sib.ItemKind)
}

func TestIntegration_ForwardCompatMarkerIsWarningExitZero(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/fm-forward-compat"
	_, err := tc.InitCampaign(dir, "fm-forward-compat", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/future/.workitem",
		"version: v1alpha99\nkind: workitem\nid: design-future-2026-07-19\ntype: design\ntitle: Future\nref: WI-dddddd\n"))

	codes, nonZero := fmValidateFindings(t, tc, dir)
	assert.False(t, nonZero, "a forward-compat marker alone must not fail validate")
	assert.Equal(t, "warning", codes["workitem.schema.forward-compat|workflow/design/future"],
		"forward-compat marker must produce a warning, not a malformed error")
	assert.NotContains(t, codes, "workitem.marker.malformed|workflow/design/future",
		"forward-compat marker must not be reported as malformed")
}

func TestIntegration_FrontmatterRefPending(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/fm-ref-pending"
	_, err := tc.InitCampaign(dir, "fm-ref-pending", "product")
	require.NoError(t, err)

	// directory marker with no ref; nested frontmatter shares the id and adds a ref
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/refpend/.workitem",
		"version: v1alpha8\nkind: workitem\nid: design-refpend-2026-07-19\ntype: design\ntitle: RefPend\n"))
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/refpend/sub/note.md",
		fmDoc("design-refpend-2026-07-19", "design", "Note", "WI-aaaaaa")))

	codes, nonZero := fmValidateFindings(t, tc, dir)
	assert.False(t, nonZero, "a pending ref alone must not fail validate")
	assert.Equal(t, "warning", codes["workitem.identity.ref-pending|workflow/design/refpend"],
		"same id with only one side's ref set must warn, not hard-conflict")
	assert.NotContains(t, codes, "workitem.identity.conflict|workflow/design/refpend",
		"a pending ref must not be reported as a hard conflict")
}

func TestIntegration_FrontmatterTypeAuthority(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/fm-type-authority"
	_, err := tc.InitCampaign(dir, "fm-type-authority", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/forced.md", fmDoc("note-forced-2026-07-19", "feature", "Forced", "WI-aaaaaa")))
	require.NoError(t, tc.WriteFile(dir+"/workflow/feature/free.md", fmDoc("note-free-2026-07-19", "feature", "Free", "WI-bbbbbb")))

	items := fmListItems(t, tc, dir)

	forced, ok := fmItemByPath(items, "workflow/design/forced.md")
	require.True(t, ok, "frontmatter doc under design/ must be discovered")
	assert.Equal(t, "design", forced.WorkflowType, "path type forces design over the frontmatter's feature type")

	free, ok := fmItemByPath(items, "workflow/feature/free.md")
	require.True(t, ok, "frontmatter doc outside path-typed trees must be discovered")
	assert.Equal(t, "feature", free.WorkflowType, "outside path-typed trees the frontmatter type is authoritative")
}

func TestIntegration_FrontmatterShortCircuit(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/fm-shortcircuit"
	_, err := tc.InitCampaign(dir, "fm-shortcircuit", "product")
	require.NoError(t, err)

	// Leading --- but the block is invalid YAML and lacks kind: workitem, so the
	// kind pre-check short-circuits before yaml.Unmarshal: list succeeds and the
	// file is not discovered.
	require.NoError(t, tc.WriteFile(dir+"/workflow/notes/bad.md", "---\nnot: [valid: yaml\ntitle: whatever\n---\n\n# Bad\n"))

	items := fmListItems(t, tc, dir)
	_, found := fmItemByPath(items, "workflow/notes/bad.md")
	assert.False(t, found, "a file without kind: workitem frontmatter must not be discovered")
}

func TestIntegration_FrontmatterIdentityConflict(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/fm-conflict"
	_, err := tc.InitCampaign(dir, "fm-conflict", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/dir/.workitem", fmMarker("design-dir-2026-07-19", "design", "WI-aaaaaa")))
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/dir/sub/nested.md", fmDoc("note-other-2026-07-19", "design", "Nested", "WI-bbbbbb")))

	codes, nonZero := fmValidateFindings(t, tc, dir)
	assert.True(t, nonZero, "validate must exit non-zero on an identity conflict")
	assert.Equal(t, "error", codes["workitem.identity.conflict|workflow/design/dir"],
		"expected an error-severity identity conflict on the directory")
}

func TestIntegration_FrontmatterTypeMismatchValidate(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/fm-type-mismatch"
	_, err := tc.InitCampaign(dir, "fm-type-mismatch", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/mismatch.md", fmDoc("note-mm-2026-07-19", "feature", "Mismatch", "WI-aaaaaa")))

	codes, nonZero := fmValidateFindings(t, tc, dir)
	assert.True(t, nonZero, "validate must exit non-zero on a frontmatter type mismatch")
	assert.Equal(t, "error", codes["workitem.type.mismatch|workflow/design/mismatch.md"],
		"expected a type mismatch on the frontmatter file under a path-typed tree")
}
