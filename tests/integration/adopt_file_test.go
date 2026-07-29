//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_AdoptFileNoFrontmatterPreservesBody(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/adopt-file-nofm"
	_, err := tc.InitCampaign(dir, "adopt-file-nofm", "product")
	require.NoError(t, err)

	orig := "# My Notes\n\nSome body text.\nLine two.\n"
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/notes.md", orig))

	_, err = tc.RunCampInDir(dir, "workitem", "adopt", "--file", "workflow/design/notes.md", "--type", "design", "--title", "My Notes")
	require.NoError(t, err)

	result, err := tc.ReadFile(dir + "/workflow/design/notes.md")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(result, "---\nversion: v1alpha8\nkind: workitem\n"),
		"a valid frontmatter block must be prepended:\n%s", result)
	assert.True(t, strings.HasSuffix(result, orig),
		"the original body must be preserved byte-for-byte at the tail:\n%s", result)
}

func TestIntegration_AdoptFilePreservesForeignKeys(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/adopt-file-foreign"
	_, err := tc.InitCampaign(dir, "adopt-file-foreign", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/hugo.md",
		"---\ntitle: Launch checklist\ndate: 2026-01-02\ndraft: true\naliases: [launch-todo]\n---\n\n- [ ] cut rc\n"))

	_, err = tc.RunCampInDir(dir, "workitem", "adopt", "--file", "workflow/design/hugo.md", "--type", "design")
	require.NoError(t, err)

	result, err := tc.ReadFile(dir + "/workflow/design/hugo.md")
	require.NoError(t, err)
	assert.Contains(t, result, "title: Launch checklist", "foreign title preserved")
	assert.Contains(t, result, "date: 2026-01-02", "foreign date preserved")
	assert.Contains(t, result, "draft: true", "foreign bool preserved untyped")
	assert.Contains(t, result, "aliases: [launch-todo]", "foreign flow sequence preserved")
	assert.Contains(t, result, "kind: workitem", "camp keys merged in")
	assert.True(t, strings.HasSuffix(result, "\n- [ ] cut rc\n"), "body preserved")
}

func TestIntegration_AdoptFileForeignTagsRefusesWithoutForce(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/adopt-file-tags"
	_, err := tc.InitCampaign(dir, "adopt-file-tags", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/tagged.md",
		"---\ntitle: Tagged\ntags: [personal, blog]\n---\n\nbody\n"))

	out, err := tc.RunCampInDir(dir, "workitem", "adopt", "--file", "workflow/design/tagged.md", "--type", "design")
	require.Error(t, err, "a foreign tags: key must refuse without --force: %s", out)
	assert.Contains(t, out, "--force", "the error must tell the user how to proceed")

	_, err = tc.RunCampInDir(dir, "workitem", "adopt", "--file", "workflow/design/tagged.md", "--type", "design", "--force", "--tag", "campy")
	require.NoError(t, err, "--force must allow the stamp")
	result, err := tc.ReadFile(dir + "/workflow/design/tagged.md")
	require.NoError(t, err)
	assert.Contains(t, result, "kind: workitem", "camp keys added with --force")
	assert.Contains(t, result, "personal", "--force unions conforming foreign tags (no silent data loss)")
	assert.Contains(t, result, "blog", "--force unions conforming foreign tags (no silent data loss)")
	assert.Contains(t, result, "campy", "--force adds the new --tag values to the union")
}

func TestIntegration_AdoptFileForeignTagsDropsNonConforming(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/adopt-file-tags-nc"
	_, err := tc.InitCampaign(dir, "adopt-file-tags-nc", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/mixed.md",
		"---\ntitle: Mixed\ntags: [personal, 42]\n---\n\nbody\n"))

	out, err := tc.RunCampInDir(dir, "workitem", "adopt", "--file", "workflow/design/mixed.md", "--type", "design", "--force")
	require.NoError(t, err, "--force must succeed: %s", out)
	assert.Contains(t, out, "42", "a non-conforming foreign tag value must be reported as dropped")
	assert.Contains(t, out, "dropped", "the drop notice must be explicit")

	result, err := tc.ReadFile(dir + "/workflow/design/mixed.md")
	require.NoError(t, err)
	assert.Contains(t, result, "personal", "a conforming foreign tag survives")
	assert.NotContains(t, result, "- 42", "a non-conforming int must not be written as a tag")
}

func TestIntegration_AdoptFileReflowWarning(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/adopt-file-reflow"
	_, err := tc.InitCampaign(dir, "adopt-file-reflow", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/four.md",
		"---\ntitle: Four\nnested:\n    a: 1\n    b: 2\n---\n\nbody\n"))
	out, err := tc.RunCampInDir(dir, "workitem", "adopt", "--file", "workflow/design/four.md", "--type", "design")
	require.NoError(t, err, "%s", out)
	assert.Contains(t, out, "4-space indentation", "a non-2-space block must warn about the reflow")

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/two.md",
		"---\ntitle: Two\nnested:\n  a: 1\n---\n\nbody\n"))
	out, err = tc.RunCampInDir(dir, "workitem", "adopt", "--file", "workflow/design/two.md", "--type", "design")
	require.NoError(t, err, "%s", out)
	assert.NotContains(t, out, "indentation", "a 2-space block must not warn")
}

func TestIntegration_AdoptFileIdentityConflict(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/adopt-file-conflict"
	_, err := tc.InitCampaign(dir, "adopt-file-conflict", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/wi.md",
		"---\nversion: v1alpha8\nkind: workitem\nid: design-wi-2026-07-19\ntype: design\ntitle: WI\nref: WI-abc123\n---\n\n# WI\n"))

	out, err := tc.RunCampInDir(dir, "workitem", "adopt", "--file", "workflow/design/wi.md", "--type", "design", "--id", "design-other-2026-07-19")
	require.Error(t, err, "a conflicting --id must refuse: %s", out)
	assert.Contains(t, out, "design-wi-2026-07-19", "the error must name the existing id")

	_, err = tc.RunCampInDir(dir, "workitem", "adopt", "--file", "workflow/design/wi.md", "--type", "design")
	require.NoError(t, err, "re-adopting with no --id override must succeed as an update")
}

func TestIntegration_AdoptFileDeterministic(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/adopt-file-determ"
	_, err := tc.InitCampaign(dir, "adopt-file-determ", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/det.md",
		"---\ntitle: Det\ndraft: false\n---\n\nbody line\n"))

	_, err = tc.RunCampInDir(dir, "workitem", "adopt", "--file", "workflow/design/det.md", "--type", "design", "--tag", "a", "--tag", "b")
	require.NoError(t, err)
	run1, err := tc.ReadFile(dir + "/workflow/design/det.md")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(dir, "workitem", "adopt", "--file", "workflow/design/det.md", "--type", "design", "--tag", "a", "--tag", "b")
	require.NoError(t, err)
	run2, err := tc.ReadFile(dir + "/workflow/design/det.md")
	require.NoError(t, err)

	assert.Equal(t, run1, run2, "re-running adopt --file with identical flags must be byte-identical (diff-clean)")
}

func TestIntegration_CreateFileRefusesExisting(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/create-file-exists"
	_, err := tc.InitCampaign(dir, "create-file-exists", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/taken.md", "# already here\n"))

	out, err := tc.RunCampInDir(dir, "workitem", "create", "--file", "workflow/design/taken.md", "--type", "design", "--title", "Taken")
	require.Error(t, err, "create --file on an existing path must refuse: %s", out)
	assert.Contains(t, out, "already exists", "the error must explain the existing-target guard")
}
