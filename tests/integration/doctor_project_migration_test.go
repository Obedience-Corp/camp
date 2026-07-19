//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedDesignWorkitemWithID(t *testing.T, tc *TestContainer, dir, slug, id string) {
	t.Helper()
	out, err := tc.RunCampInDir(dir, "workitem", "create", slug, "--type", "design", "--title", slug, "--id", id)
	require.NoError(t, err, "create %s: %s", slug, out)
}

func writeRelatedProjectLinks(t *testing.T, tc *TestContainer, dir string, entries [][3]string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("version: workitem-links/v1alpha1\nlinks:\n")
	for _, e := range entries {
		fmt.Fprintf(&sb,
			"  - {id: %s, workitem_id: %s, scope: {kind: project, path: %s}, role: related, created_at: 2026-07-19T10:00:00-06:00, created_by: test}\n",
			e[0], e[1], e[2])
	}
	require.NoError(t, tc.WriteFile(dir+"/.campaign/workitems/links.yaml", sb.String()))
}

func doctorFindings(t *testing.T, out string) []doctorFinding {
	t.Helper()
	start := strings.Index(out, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON in doctor output: %s", out)
	var report doctorReport
	require.NoError(t, json.NewDecoder(strings.NewReader(out[start:])).Decode(&report), "doctor --json parse: %s", out)
	return report.Findings
}

func setupRelatedProjectMigration(t *testing.T, tc *TestContainer, dir string) {
	t.Helper()
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "pa")
	seedProject(t, tc, dir, "pb")
	seedDesignWorkitemWithID(t, tc, dir, "wa", "design-wa-2026-07-19")
	seedDesignWorkitemWithID(t, tc, dir, "wb", "design-wb-2026-07-19")
	writeRelatedProjectLinks(t, tc, dir, [][3]string{
		{"lnk_20260719_000001", "design-wa-2026-07-19", "projects/pa"},
		{"lnk_20260719_000002", "design-wb-2026-07-19", "projects/pb"},
	})
}

func TestIntegration_DoctorReportsRelatedProjectDeprecated(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-migrate-report"
	setupRelatedProjectMigration(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workitem", "doctor", "--json")
	require.NoError(t, err, "report mode with warnings only must exit 0: %s", out)

	n := 0
	for _, f := range doctorFindings(t, out) {
		if f.Code == "workitem.link.related-project-deprecated" {
			assert.Equal(t, "warning", f.Severity)
			n++
		}
	}
	assert.Equal(t, 2, n, "expected exactly one deprecated finding per related-project row")
}

func TestIntegration_DoctorFixMigratesRelatedProject(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-migrate-fix"
	setupRelatedProjectMigration(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workitem", "doctor", "--fix")
	require.NoError(t, err, "--fix: %s", out)

	linksYAML, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.NotContains(t, linksYAML, "role: related", "zero related+project rows remain after --fix")

	wa, err := tc.ReadFile(dir + "/workflow/design/wa/.workitem")
	require.NoError(t, err)
	assert.Contains(t, wa, "projects/pa", "the project path is migrated into the marker")
	wb, err := tc.ReadFile(dir + "/workflow/design/wb/.workitem")
	require.NoError(t, err)
	assert.Contains(t, wb, "projects/pb")
}

func TestIntegration_DoctorFixIdempotent(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-migrate-idempotent"
	setupRelatedProjectMigration(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "workitem", "doctor", "--fix")
	require.NoError(t, err)
	out, err := tc.RunCampInDir(dir, "workitem", "doctor", "--fix")
	require.NoError(t, err, "second --fix must be a clean no-op: %s", out)

	jout, err := tc.RunCampInDir(dir, "workitem", "doctor", "--json")
	require.NoError(t, err)
	for _, f := range doctorFindings(t, jout) {
		assert.NotEqual(t, "workitem.link.related-project-deprecated", f.Code, "no deprecated findings remain")
	}
	wa, err := tc.ReadFile(dir + "/workflow/design/wa/.workitem")
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(wa, "projects/pa"), "no duplicate projects entry after a second --fix")
}

func TestIntegration_DoctorFixPreExistingOverlap(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-migrate-overlap"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "pa")
	// The workitem already lists projects/pa before migration.
	out, err := tc.RunCampInDir(dir, "workitem", "create", "wa", "--type", "design", "--title", "wa",
		"--id", "design-wa-2026-07-19", "--project", "projects/pa")
	require.NoError(t, err, "create with --project: %s", out)
	writeRelatedProjectLinks(t, tc, dir, [][3]string{{"lnk_20260719_000001", "design-wa-2026-07-19", "projects/pa"}})

	_, err = tc.RunCampInDir(dir, "workitem", "doctor", "--fix")
	require.NoError(t, err)

	wa, err := tc.ReadFile(dir + "/workflow/design/wa/.workitem")
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(wa, "projects/pa"), "a pre-existing project must not be duplicated")
	linksYAML, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.NotContains(t, linksYAML, "role: related", "the now-redundant row is still removed")
}

func TestIntegration_DoctorFixFileFrontmatterTarget(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-migrate-file"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "pa")
	require.NoError(t, tc.WriteFile(dir+"/workflow/notes/doc.md",
		"---\nversion: v1alpha8\nkind: workitem\nid: note-doc-2026-07-19\ntype: chore\ntitle: Doc\nref: WI-fa0001\ndraft: true\n---\n\n# Doc\n\nBody text.\n"))
	writeRelatedProjectLinks(t, tc, dir, [][3]string{{"lnk_20260719_000001", "note-doc-2026-07-19", "projects/pa"}})

	_, err := tc.RunCampInDir(dir, "workitem", "doctor", "--fix")
	require.NoError(t, err)

	doc, err := tc.ReadFile(dir + "/workflow/notes/doc.md")
	require.NoError(t, err)
	assert.Contains(t, doc, "projects/pa", "migrated into the file frontmatter")
	assert.Contains(t, doc, "draft: true", "a foreign frontmatter key is preserved")
	assert.Contains(t, doc, "Body text.", "the body is preserved")
	linksYAML, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.NotContains(t, linksYAML, "role: related")
}

func TestIntegration_DoctorFixFileFrontmatterProjectsAfterTags(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-migrate-after-tags"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "pa")
	// A file workitem that already carries tags: but no projects:. create/adopt
	// chain projects: after tags:, so doctor's migration must land it there too.
	require.NoError(t, tc.WriteFile(dir+"/workflow/notes/tagged.md",
		"---\nversion: v1alpha8\nkind: workitem\nid: note-tagged-2026-07-19\ntype: chore\ntitle: Tagged\nref: WI-fb0001\ntags:\n  - foo\n  - bar\ndraft: true\n---\n\n# Tagged\n\nBody.\n"))
	writeRelatedProjectLinks(t, tc, dir, [][3]string{{"lnk_20260719_000001", "note-tagged-2026-07-19", "projects/pa"}})

	_, err := tc.RunCampInDir(dir, "workitem", "doctor", "--fix")
	require.NoError(t, err)

	doc, err := tc.ReadFile(dir + "/workflow/notes/tagged.md")
	require.NoError(t, err)
	assert.Contains(t, doc, "projects/pa", "migrated into the frontmatter")
	assert.Contains(t, doc, "foo", "existing tags preserved")
	assert.Contains(t, doc, "bar")
	tagsAt := strings.Index(doc, "\ntags:")
	projectsAt := strings.Index(doc, "\nprojects:")
	draftAt := strings.Index(doc, "\ndraft:")
	require.Positive(t, projectsAt, "projects: key present")
	assert.Less(t, tagsAt, projectsAt, "projects: is stamped after tags:, matching create/adopt")
	assert.Less(t, projectsAt, draftAt, "projects: lands right after the tags block, before the trailing foreign key")
}

func TestIntegration_DoctorMissingWorkitemRowPreserved(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-migrate-missing"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "pa")
	writeRelatedProjectLinks(t, tc, dir, [][3]string{{"lnk_20260719_000001", "gone-2026-07-19", "projects/pa"}})

	out, err := tc.RunCampInDir(dir, "workitem", "doctor", "--json")
	require.NoError(t, err, "report exits 0: %s", out)
	hasDeprecated := false
	for _, f := range doctorFindings(t, out) {
		if f.Target == "link:lnk_20260719_000001" {
			assert.NotEqual(t, "workitem.link.broken", f.Code,
				"a deprecated related-project row is not also flagged broken (it is handled solely by the migration)")
			if f.Code == "workitem.link.related-project-deprecated" {
				hasDeprecated = true
			}
		}
	}
	assert.True(t, hasDeprecated, "the row is reported as deprecated")

	_, err = tc.RunCampInDir(dir, "workitem", "doctor", "--fix")
	require.NoError(t, err, "--fix with only an unmigratable row still exits 0")
	linksYAML, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, linksYAML, "lnk_20260719_000001", "an unmigratable row is preserved, never deleted")
	exists, err := tc.CheckDirExists(dir + "/workflow/design/gone")
	require.NoError(t, err)
	assert.False(t, exists, "no phantom workitem is created for a missing target")
}
