//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createFileWorkitem(t *testing.T, tc *TestContainer, campaign, wfType, slug, id, ref string) {
	t.Helper()
	content := "---\nversion: v1alpha8\nkind: workitem\nid: " + id +
		"\ntype: " + wfType + "\ntitle: " + slug +
		"\nref: " + ref + "\n---\n\n# " + slug + "\n\nBody paragraph so promote is not empty.\n"
	require.NoError(t, tc.WriteFile(campaign+"/workflow/"+wfType+"/"+slug+".md", content))
}

func findOneFile(tc *TestContainer, root, name string) (string, bool) {
	out, _, _ := tc.ExecCommand("find", root, "-name", name, "-type", "f", "-print", "-quit")
	p := strings.TrimSpace(out)
	return p, p != ""
}

func TestIntegration_FileDungeonStatuses(t *testing.T) {
	tc := GetSharedContainer(t)
	campaign := setupDungeonCampaign(t, tc, "file-dungeon")

	statuses := []string{"completed", "archived", "someday"}
	refs := map[string]string{"completed": "WI-c00001", "archived": "WI-a00002", "someday": "WI-500003"}
	for _, status := range statuses {
		id := "note-" + status + "file-2026-07-19"
		createFileWorkitem(t, tc, campaign, "design", status+"file", id, refs[status])
	}
	tc.GitOutput(t, campaign, "add", "-A")
	tc.GitOutput(t, campaign, "commit", "-m", "seed file workitems")

	for _, status := range statuses {
		id := "note-" + status + "file-2026-07-19"
		out, err := tc.RunCampInDir(campaign, "workitem", "promote", id, "--target", status)
		require.NoError(t, err, "promote %s to %s: %s", id, status, out)

		stillActive, err := tc.CheckFileExists(campaign + "/workflow/design/" + status + "file.md")
		require.NoError(t, err)
		assert.False(t, stillActive, "%sfile.md must leave the active location", status)

		found, err := checkDatedDungeonStatusItemExists(tc, campaign+"/workflow/design/dungeon/"+status, status+"file.md")
		require.NoError(t, err)
		assert.True(t, found, "%sfile.md must land under dungeon/%s by the dated convention", status, status)
	}

	// Dungeoned files are excluded from active discovery.
	listOut, err := tc.RunCampInDir(campaign, "workitem", "list", "--json")
	require.NoError(t, err)
	for _, status := range statuses {
		assert.NotContains(t, listOut, "note-"+status+"file-2026-07-19",
			"a dungeoned file must be excluded from active discovery")
	}
}

func TestIntegration_FilePromoteToDoc(t *testing.T) {
	tc := GetSharedContainer(t)
	campaign := setupDungeonCampaign(t, tc, "file-promote-doc")

	createFileWorkitem(t, tc, campaign, "design", "docme", "note-docme-2026-07-19", "WI-dc0001")
	tc.GitOutput(t, campaign, "add", "-A")
	tc.GitOutput(t, campaign, "commit", "-m", "seed file workitem")

	out, err := tc.RunCampInDir(campaign, "workitem", "promote", "note-docme-2026-07-19", "--target", "doc")
	require.NoError(t, err, "promote to doc: %s", out)

	// Copied to docs/<slug>/<file>.md, not an empty docs/<slug>/ directory.
	copied, err := tc.CheckFileExists(campaign + "/docs/docme/docme.md")
	require.NoError(t, err)
	assert.True(t, copied, "the file must be copied to docs/docme/docme.md")

	// Source left the active location (shelved to dungeon/completed).
	stillActive, err := tc.CheckFileExists(campaign + "/workflow/design/docme.md")
	require.NoError(t, err)
	assert.False(t, stillActive, "the source must be shelved out of the active location")

	// The shelved source carries the promotion stamp with its body intact.
	shelved, ok := findOneFile(tc, campaign+"/workflow/design/dungeon/completed", "docme.md")
	require.True(t, ok, "the shelved source file must exist under dungeon/completed")
	content, err := tc.ReadFile(shelved)
	require.NoError(t, err)
	assert.Contains(t, content, "promoted_to: docs/docme", "promoted_to must be stamped in frontmatter")
	assert.Contains(t, content, "promoted_at:", "promoted_at must be stamped in frontmatter")
	assert.Contains(t, content, "Body paragraph", "the body must be preserved")
}

func TestIntegration_FilePromoteToFestival(t *testing.T) {
	if !festAvailable {
		t.Skip("fest CLI not available in container")
	}
	tc := GetSharedContainer(t)
	campaign := setupDungeonCampaign(t, tc, "file-promote-festival")
	_, _, err := tc.ExecCommand("sh", "-c",
		"mkdir -p "+campaign+"/festivals/.festival/templates "+campaign+"/festivals/.festival/.state "+campaign+"/festivals/planning")
	require.NoError(t, err)

	createFileWorkitem(t, tc, campaign, "design", "festme", "note-festme-2026-07-19", "WI-fe0001")
	tc.GitOutput(t, campaign, "add", "-A")
	tc.GitOutput(t, campaign, "commit", "-m", "seed file workitem")

	out, err := tc.RunCampInDir(campaign, "workitem", "promote", "note-festme-2026-07-19", "--target", "festival")
	require.NoError(t, err, "promote to festival: %s", out)

	// The festival ingest receives just the file (input_specs/<slug>/<file>.md),
	// not an empty directory tree.
	ingested, ok := findOneFile(tc, campaign+"/festivals", "festme.md")
	require.True(t, ok, "the file must be copied into the festival ingest")
	assert.Contains(t, ingested, "input_specs", "the copy must land under 001_INGEST/input_specs")
}

func TestIntegration_GatherMixedDirAndFile(t *testing.T) {
	tc := GetSharedContainer(t)
	campaign := "/campaigns/gather-mixed"
	_, err := tc.InitCampaign(campaign, "gather-mixed", "product")
	require.NoError(t, err)

	// One directory-shaped source and one file-shaped source.
	createDesignWorkitem(t, tc, campaign, "dir-src", "Dir Src", "# Dir Src\n\nA directory design.\n")
	createFileWorkitem(t, tc, campaign, "design", "file-src", "note-file-src-2026-07-19", "WI-fa0001")
	tc.GitOutput(t, campaign, "add", "-A")
	tc.GitOutput(t, campaign, "commit", "-m", "seed mixed sources")

	out, err := tc.RunCampInDir(campaign, "gather", "design", "dir-src", "note-file-src-2026-07-19", "--title", "Mixed Pkg")
	require.NoError(t, err, "gather mixed: %s", out)

	// The file source's frontmatter is stamped with gathered_into, like a
	// directory source's .workitem marker.
	movedFile, ok := findOneFile(tc, campaign+"/workflow/design/mixed-pkg", "file-src.md")
	require.True(t, ok, "the file source must move into the gathered package")
	fileContent, err := tc.ReadFile(movedFile)
	require.NoError(t, err)
	assert.Contains(t, fileContent, "gathered_into: ", "the file source frontmatter must be stamped")
	assert.Contains(t, fileContent, "gathered_at:", "the file source frontmatter must be stamped")

	dirMarker, err := tc.ReadFile(campaign + "/workflow/design/mixed-pkg/dir-src/.workitem")
	require.NoError(t, err)
	assert.Contains(t, dirMarker, "gathered_into: ", "the directory source marker must be stamped identically")

	readme, err := tc.ReadFile(campaign + "/workflow/design/mixed-pkg/README.md")
	require.NoError(t, err)
	assert.Contains(t, readme, "Dir Src", "the README must reference the directory source")
	assert.Contains(t, readme, "file-src", "the README must reference the file source")
}
