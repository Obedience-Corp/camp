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

// seedIntent captures an intent through the real CLI and returns the bare
// frontmatter id `camp workitem id` reports for it.
func seedIntent(t *testing.T, tc *TestContainer, dir, title string) string {
	t.Helper()
	out, err := tc.RunCampInDir(dir, "intent", "add", title, "--no-commit")
	require.NoError(t, err, "intent add: %s", out)

	listed, err := tc.RunCampInDir(dir, "workitem", "--json")
	require.NoError(t, err, "workitem --json: %s", listed)
	var payload struct {
		Items []struct {
			WorkflowType string `json:"workflow_type"`
			SourceID     string `json:"source_id"`
			Title        string `json:"title"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(listed), &payload), "raw=%s", listed)
	for _, wi := range payload.Items {
		if wi.WorkflowType == "intent" && wi.Title == title {
			require.NotEmpty(t, wi.SourceID, "intent must expose its frontmatter id")
			return wi.SourceID
		}
	}
	t.Fatalf("no intent titled %q in workitem --json: %s", title, listed)
	return ""
}

// TestIntegration_IntentPrimaryLinkLifecycle is the containerized regression for
// the intent-link defect: an intent must link to a worktree scope by its bare
// frontmatter id, survive on disk with the key form in workitem_key, resolve
// back from inside that scope, pass doctor, and unlink again.
func TestIntegration_IntentPrimaryLinkLifecycle(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-intent-links"
	initLinksCampaign(t, tc, dir)

	const title = "Intents cannot be primary-linked"
	intentID := seedIntent(t, tc, dir, title)

	// `camp workitem id` must print the bare id, not the "intent:<path>" key:
	// the key form is what the links validator rejects.
	idOut, err := tc.RunCampInDir(dir, "workitem", "id", intentID)
	require.NoError(t, err, "workitem id: %s", idOut)
	assert.Equal(t, intentID, strings.TrimSpace(idOut))
	assert.NotContains(t, idOut, "intent:", "stdout id must be the bare id")

	keyOut, err := tc.RunCampInDir(dir, "workitem", "id", "--key", intentID)
	require.NoError(t, err, "workitem id --key: %s", keyOut)
	assert.Contains(t, keyOut, "intent:.campaign/intents/")

	worktreeScope := "projects/worktrees/camp/intent-links"
	_, _, err = tc.ExecCommand("mkdir", "-p", dir+"/"+worktreeScope)
	require.NoError(t, err)

	out, err := tc.RunCampInDir(dir, "workitem", "link", intentID, "--worktree", "camp/intent-links")
	require.NoError(t, err, "workitem link intent: %s", out)
	assert.Contains(t, out, intentID)

	linksYAML, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, linksYAML, "workitem_id: "+intentID,
		"workitem_id must hold the bare id")
	assert.Contains(t, linksYAML, "workitem_key: intent:.campaign/intents/",
		"workitem_key must hold the path-derived key form")

	// The link must be findable by the intent that owns it.
	out, err = tc.RunCampInDir(dir, "workitem", "links", intentID)
	require.NoError(t, err, "workitem links <intent>: %s", out)
	assert.Contains(t, out, worktreeScope)

	// Round-trip: from inside the linked scope the resolver must land back on
	// the intent, which is what gives camp p commit its workitem context there.
	out, err = tc.RunCampInDir(dir+"/"+worktreeScope, "workitem", "resolve", "--json")
	require.NoError(t, err, "resolve inside linked worktree: %s", out)
	src, resolution := extractResolveSource(t, out)
	assert.Equal(t, "link", src, "linked worktree should resolve via the link tier")
	wi, _ := resolution["workitem"].(map[string]any)
	require.NotNil(t, wi, "resolution must carry a workitem: %s", out)
	assert.Equal(t, "intent", wi["workflow_type"])
	assert.Equal(t, intentID, wi["source_id"])

	// A link camp itself wrote must not be a doctor finding.
	out, err = tc.RunCampInDir(dir, "workitem", "doctor")
	require.NoError(t, err, "doctor: %s", out)
	assert.NotContains(t, out, intentID, "intent link must not be reported broken: %s", out)

	out, err = tc.RunCampInDir(dir, "workitem", "unlink", intentID, "--worktree", "camp/intent-links")
	require.NoError(t, err, "unlink intent: %s", out)
	out, err = tc.RunCampInDir(dir, "workitem", "links")
	require.NoError(t, err, "links post-unlink: %s", out)
	assert.Contains(t, out, "no links")
}

// TestIntegration_WorktreeAdd_IntentLinksNotDangles is the sibling of
// TestIntegration_WorktreeAdd_UnadoptedDesignDirErrors and covers the exact
// failure the intent reported: the link error arrived only after the worktree
// existed, so `camp project worktree add --workitem <intent>` left an unlinked
// worktree behind. An intent carries its own id, so the link must now succeed.
func TestIntegration_WorktreeAdd_IntentLinksNotDangles(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/worktree-add-intent"
	initCommitTagsCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "project", "new", "demo-app")
	require.NoError(t, err, "camp project new")

	intentID := seedIntent(t, tc, dir, "Intent drives a worktree")

	out, err := tc.RunCampInDir(dir, "project", "worktree", "add", "intent-wt",
		"--project", "demo-app", "--workitem", intentID)
	require.NoError(t, err, "worktree add --workitem <intent>: %s", out)
	assert.NotContains(t, out, "must not contain",
		"the workitem_id path-segment rule must not surface here: %s", out)

	wtRel := "projects/worktrees/demo-app/intent-wt"
	exists, err := tc.CheckDirExists(dir + "/" + wtRel)
	require.NoError(t, err)
	require.True(t, exists, "worktree should exist at %s; output:\n%s", wtRel, out)

	linksYAML, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, linksYAML, "workitem_id: "+intentID,
		"the created worktree must be linked to the intent, not left dangling")
	assert.Contains(t, linksYAML, wtRel)

	// The link must be live from inside the worktree, which is what gives
	// camp p commit its workitem context there.
	resolveOut, err := tc.RunCampInDir(dir+"/"+wtRel, "workitem", "resolve", "--json")
	require.NoError(t, err, "resolve inside intent worktree: %s", resolveOut)
	src, resolution := extractResolveSource(t, resolveOut)
	assert.Equal(t, "link", src)
	wi, _ := resolution["workitem"].(map[string]any)
	require.NotNil(t, wi, "resolution must carry a workitem: %s", resolveOut)
	assert.Equal(t, intentID, wi["source_id"])
}
