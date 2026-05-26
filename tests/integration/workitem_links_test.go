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

const linksCampaignDir = "/test/workitem-links"

func initLinksCampaign(t *testing.T, tc *TestContainer, dir string) {
	t.Helper()
	_, err := tc.RunCamp(
		"init", dir,
		"--name", "Workitem Links",
		"--type", "product",
		"-d", "Workitem link integration",
		"-m", "Verify link surface",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")
}

func seedDesignWorkitem(t *testing.T, tc *TestContainer, dir, slug string) {
	t.Helper()
	out, err := tc.RunCampInDir(dir, "workitem", "create", slug, "--type", "design", "--title", slug)
	require.NoError(t, err, "workitem create: %s", out)
}

func seedProject(t *testing.T, tc *TestContainer, dir, name string) {
	t.Helper()
	_, _, err := tc.ExecCommand("mkdir", "-p", dir+"/projects/"+name)
	require.NoError(t, err)
}

func TestIntegration_LinkLifecycle(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := linksCampaignDir
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "camp-timeline")
	seedDesignWorkitem(t, tc, dir, "timeline")

	out, err := tc.RunCampInDir(dir,
		"workitem", "link", "timeline", "--project", "camp-timeline")
	require.NoError(t, err, "workitem link: %s", out)
	assert.Contains(t, out, "linked")

	linksYAML, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, linksYAML, "version: workitem-links/v1alpha1")
	assert.Contains(t, linksYAML, "projects/camp-timeline")

	out, err = tc.RunCampInDir(dir, "workitem", "links")
	require.NoError(t, err, "links: %s", out)
	assert.Contains(t, out, "camp-timeline")

	out, err = tc.RunCampInDir(dir,
		"workitem", "unlink", "timeline", "projects/camp-timeline")
	require.NoError(t, err, "unlink: %s", out)

	out, err = tc.RunCampInDir(dir, "workitem", "links")
	require.NoError(t, err, "links post-unlink: %s", out)
	assert.Contains(t, out, "no links")
}

func TestIntegration_LinkJSONContracts(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-links-json"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "camp-timeline")
	seedDesignWorkitem(t, tc, dir, "timeline")

	out, err := tc.RunCampInDir(dir,
		"workitem", "link", "timeline", "--project", "camp-timeline", "--json")
	require.NoError(t, err, "workitem link --json: %s", out)
	assert.NotContains(t, out, "WorkitemID")
	assert.NotContains(t, out, "CreatedAt")
	assert.NotContains(t, out, "\"Kind\"")

	var linkPayload struct {
		SchemaVersion string `json:"schema_version"`
		Link          struct {
			WorkitemID string `json:"workitem_id"`
			Scope      struct {
				Kind string `json:"kind"`
				Path string `json:"path"`
			} `json:"scope"`
		} `json:"link"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &linkPayload), "raw=%s", out)
	assert.Equal(t, "workitem-links/v1alpha1", linkPayload.SchemaVersion)
	assert.NotEmpty(t, linkPayload.Link.WorkitemID)
	assert.Equal(t, "project", linkPayload.Link.Scope.Kind)
	assert.Equal(t, "projects/camp-timeline", linkPayload.Link.Scope.Path)

	out, err = tc.RunCampInDir(dir, "workitem", "links", "--json")
	require.NoError(t, err, "workitem links --json: %s", out)
	var linksPayload struct {
		Links []struct {
			WorkitemID string `json:"workitem_id"`
			Scope      struct {
				Kind string `json:"kind"`
				Path string `json:"path"`
			} `json:"scope"`
		} `json:"links"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &linksPayload), "raw=%s", out)
	require.Len(t, linksPayload.Links, 1)
	assert.Equal(t, "project", linksPayload.Links[0].Scope.Kind)
	assert.NotContains(t, out, "WorkitemID")

	out, err = tc.RunCampInDir(dir, "workitem", "current", "timeline", "--json")
	require.NoError(t, err, "workitem current --json: %s", out)
	var currentPayload struct {
		Current struct {
			WorkitemID string `json:"workitem_id"`
		} `json:"current"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &currentPayload), "raw=%s", out)
	assert.Equal(t, linkPayload.Link.WorkitemID, currentPayload.Current.WorkitemID)
	assert.NotContains(t, out, "WorkitemID")
}

func TestIntegration_LinkJSONErrorEnvelope(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-link-json-error"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "camp-timeline")

	stdoutPath := "/tmp/workitem-link-json-error-stdout"
	stderrPath := "/tmp/workitem-link-json-error-stderr"
	_, code, err := tc.ExecCommand("sh", "-c",
		"cd "+dir+" && /camp workitem link missing --project camp-timeline --json >"+stdoutPath+" 2>"+stderrPath)
	require.NoError(t, err)
	require.Equal(t, 2, code)

	stdout, err := tc.ReadFile(stdoutPath)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	stderr, err := tc.ReadFile(stderrPath)
	require.NoError(t, err)
	assert.NotContains(t, stderr, "Usage:")

	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Error         struct {
			Code     string `json:"code"`
			Message  string `json:"message"`
			Hint     string `json:"hint"`
			ExitCode int    `json:"exit_code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(stderr), &envelope), "stderr=%s", stderr)
	assert.Equal(t, "workitem-links/v1alpha1", envelope.SchemaVersion)
	assert.Equal(t, 2, envelope.Error.ExitCode)
	assert.NotEmpty(t, envelope.Error.Code)
	assert.NotEmpty(t, envelope.Error.Message)
}

func TestIntegration_ResolverTiers(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/resolver-tiers"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "camp-timeline")
	seedDesignWorkitem(t, tc, dir, "timeline")

	// Tier 1 (explicit): wins always.
	out, err := tc.RunCampInDir(dir,
		"workitem", "resolve", "--workitem", "timeline", "--json")
	require.NoError(t, err, "resolve --workitem: %s", out)
	src, _ := extractResolveSource(t, out)
	assert.Equal(t, "explicit", src, "explicit tier should win")

	// Tier 2 (ancestor): cd into workitem directory.
	out, err = tc.RunCampInDir(dir+"/workflow/design/timeline",
		"workitem", "resolve", "--json")
	require.NoError(t, err)
	src, _ = extractResolveSource(t, out)
	assert.Equal(t, "ancestor", src)

	// Tier 3 (link): create a link, cd into linked project.
	_, err = tc.RunCampInDir(dir,
		"workitem", "link", "timeline", "--project", "camp-timeline")
	require.NoError(t, err)
	out, err = tc.RunCampInDir(dir+"/projects/camp-timeline",
		"workitem", "resolve", "--json")
	require.NoError(t, err)
	src, _ = extractResolveSource(t, out)
	assert.Equal(t, "link", src)

	// Tier 4 (festival): seed festival dir + festival-scoped link.
	_, _, err = tc.ExecCommand("mkdir", "-p", dir+"/festivals/active/CT0001")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir,
		"workitem", "link", "timeline", "--festival", "CT0001")
	require.NoError(t, err)
	out, err = tc.RunCampInDir(dir,
		"workitem", "resolve", "--festival", "CT0001", "--json")
	require.NoError(t, err)
	src, _ = extractResolveSource(t, out)
	assert.Equal(t, "festival", src, "festival tier should win when explicit/ancestor/link miss")

	// Tier 5 (current): clear other links, set current, resolve from docs/.
	_, err = tc.RunCampInDir(dir, "workitem", "unlink", "timeline", "--all")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workitem", "current", "timeline")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("mkdir", "-p", dir+"/docs")
	require.NoError(t, err)
	out, err = tc.RunCampInDir(dir+"/docs", "workitem", "resolve", "--json")
	require.NoError(t, err)
	src, _ = extractResolveSource(t, out)
	assert.Equal(t, "current", src)

	// Tier 6 (none): clear current.
	_, err = tc.RunCampInDir(dir, "workitem", "current", "--clear")
	require.NoError(t, err)
	out, err = tc.RunCampInDir(dir+"/docs", "workitem", "resolve", "--json")
	require.NoError(t, err)
	src, _ = extractResolveSource(t, out)
	assert.Equal(t, "none", src)
}

func TestIntegration_ResolverQuestID(t *testing.T) {
	// quest_id propagation lands in sequence 03 task 01. Until then the
	// .workitem file has no quest_id field, so the resolver returns empty.
	// Keep the test as a placeholder so the contract reappears once the
	// dependency ships.
	t.Skip("quest_id field on .workitem is delivered by sequence 03 task 01")
}

func TestIntegration_DoctorBrokenLinks(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-broken"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "camp-timeline")
	seedDesignWorkitem(t, tc, dir, "timeline")
	_, err := tc.RunCampInDir(dir,
		"workitem", "link", "timeline", "--project", "camp-timeline")
	require.NoError(t, err)

	// 1. Delete the workitem -> broken-link.
	_, _, err = tc.ExecCommand("rm", "-rf", dir+"/workflow/design/timeline")
	require.NoError(t, err)
	out, err := tc.RunCampInDir(dir, "workitem", "doctor")
	require.Error(t, err, "doctor must exit non-zero on errors: %s", out)
	assert.Contains(t, out, "workitem.link.broken")

	// 2. Replace scope.path with ../escape -> out-of-bounds via schema-violation.
	_, _, err = tc.ExecCommand("sh", "-c",
		"sed -i 's|projects/camp-timeline|../escape|' "+dir+"/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	out, _ = tc.RunCampInDir(dir, "workitem", "doctor")
	// Note: doctor surfaces either out-of-bounds or schema.violation depending on
	// which validator catches it first; assert at least one of them is present.
	assert.True(t,
		strings.Contains(out, "workitem.scope.out-of-bounds") || strings.Contains(out, "workitem.schema.violation"),
		"expected out-of-bounds or schema violation finding in: %s", out)

	// 3. Corrupt YAML -> error from Load surfaces via wrapped error (not a
	// finding — the registry won't load at all).
	require.NoError(t, tc.WriteFile(dir+"/.campaign/workitems/links.yaml", "not: yaml: ::\n"))
	out, err = tc.RunCampInDir(dir, "workitem", "doctor")
	require.Error(t, err, "corrupt YAML must error: %s", out)

	// 4. Rebuild a valid registry and add a duplicate primary by hand.
	require.NoError(t, tc.WriteFile(dir+"/.campaign/workitems/links.yaml", `version: workitem-links/v1alpha1
links:
  - id: lnk_20260524_aaaaaa
    workitem_id: design-timeline-2026-05-24
    workitem_key: design:workflow/design/timeline
    scope:
      kind: project
      path: projects/camp-timeline
    role: primary
    created_at: 2026-05-24T19:00:00Z
    created_by: test
  - id: lnk_20260524_bbbbbb
    workitem_id: design-other-2026-05-24
    workitem_key: design:workflow/design/other
    scope:
      kind: project
      path: projects/camp-timeline
    role: primary
    created_at: 2026-05-24T19:01:00Z
    created_by: test
`))
	out, err = tc.RunCampInDir(dir, "workitem", "doctor")
	require.Error(t, err)
	assert.Contains(t, out, "workitem.link.duplicate-primary")

	// 5. stale-quest-id requires the quest_id field shipped by sequence 03;
	// this assertion is deferred to that sequence's integration tests.
}

func TestIntegration_DoctorFix(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-fix"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "camp-timeline")
	seedDesignWorkitem(t, tc, dir, "timeline")
	_, err := tc.RunCampInDir(dir,
		"workitem", "link", "timeline", "--project", "camp-timeline")
	require.NoError(t, err)

	// Orphan the workitem.
	_, _, err = tc.ExecCommand("rm", "-rf", dir+"/workflow/design/timeline")
	require.NoError(t, err)

	// Pre-fix: doctor reports broken-link and exits non-zero.
	_, err = tc.RunCampInDir(dir, "workitem", "doctor")
	require.Error(t, err)

	// Fix: should clear the orphan link.
	out, err := tc.RunCampInDir(dir, "workitem", "doctor", "--fix")
	require.NoError(t, err, "doctor --fix should clear: %s", out)

	// Re-run: doctor should be clean.
	out, err = tc.RunCampInDir(dir, "workitem", "doctor")
	require.NoError(t, err, "post-fix doctor should be clean: %s", out)

	// Links file no longer references the orphan.
	linksYAML, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.NotContains(t, linksYAML, "timeline")
}

// extractResolveSource pulls .resolution.source out of a resolve --json
// payload. Returns the source string and the full resolution payload for
// downstream assertions.
func extractResolveSource(t *testing.T, raw string) (string, map[string]any) {
	t.Helper()
	var envelope struct {
		Resolution map[string]any `json:"resolution"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &envelope), "raw=%s", raw)
	src, _ := envelope.Resolution["source"].(string)
	return src, envelope.Resolution
}
