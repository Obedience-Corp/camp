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

func TestFreshConfigure_SetPruneOffIsIdempotent(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "fresh-configure-set-prune")

	out, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "set", "prune", "--action", "off", "--json")
	require.NoError(t, err, "first prune off:\n%s", out)
	first := parseFreshSetJSON(t, out)
	assert.Equal(t, "fresh-workflow/v1", first["schema_version"])
	assert.Equal(t, "prune", first["key"])
	assert.Equal(t, true, first["changed"])

	yaml, err := tc.ReadFile(campaignPath + "/.campaign/settings/fresh.yaml")
	require.NoError(t, err)
	assert.Contains(t, yaml, "prune: false")

	out, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "set", "prune", "--action", "off", "--json")
	require.NoError(t, err, "second prune off:\n%s", out)
	again := parseFreshSetJSON(t, out)
	assert.Equal(t, false, again["changed"], "unchanged write must not rewrite the file")
}

func TestFreshConfigure_SetRefusesProjectPrune(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "fresh-configure-set-project-prune")

	out, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "set", "prune", "--action", "off", "--project", "test-project", "--json")
	require.Error(t, err, "project prune should fail:\n%s", out)
	assert.Contains(t, out, "camp-wide")
}

func TestFreshConfigure_SetProjectBranchThenInherit(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "fresh-configure-set-branch")

	out, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "set", "branch", "--action", "branch", "--value", "feat/api", "--project", "test-project", "--json")
	require.NoError(t, err, "set project branch:\n%s", out)
	payload := parseFreshSetJSON(t, out)
	assert.Equal(t, true, payload["changed"])

	yaml, err := tc.ReadFile(campaignPath + "/.campaign/settings/fresh.yaml")
	require.NoError(t, err)
	assert.Contains(t, yaml, "feat/api")

	out, err = tc.RunCampInDir(campaignPath, "fresh", "show-workflow", "test-project", "--json")
	require.NoError(t, err, "show-workflow --json:\n%s", out)
	workflow := parseJSONObject(t, out)
	assert.Equal(t, "fresh-workflow/v1", workflow["schema_version"])
	assert.Equal(t, "test-project", workflow["project"])

	out, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "set", "branch", "--action", "inherit", "--project", "test-project", "--json")
	require.NoError(t, err, "inherit project branch:\n%s", out)

	yaml, err = tc.ReadFile(campaignPath + "/.campaign/settings/fresh.yaml")
	require.NoError(t, err)
	assert.NotContains(t, yaml, "feat/api", "inheriting the last project key should drop the override")
}

func TestFreshConfigure_EditFollowUpKeepsOrder(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "fresh-configure-edit")

	_, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "install", "--run", "npm install")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "build", "--run", "npm run build")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "edit", "install", "--run", "npm ci", "--name", "bootstrap", "--continue-on-error")
	require.NoError(t, err, "edit follow-up:\n%s", out)
	assert.Contains(t, out, "bootstrap")

	out, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "show")
	require.NoError(t, err, "configure show:\n%s", out)
	installIdx := strings.Index(out, "bootstrap")
	buildIdx := strings.Index(out, "build")
	require.GreaterOrEqual(t, installIdx, 0, "edited follow-up missing:\n%s", out)
	require.GreaterOrEqual(t, buildIdx, 0, "later follow-up missing:\n%s", out)
	assert.Less(t, installIdx, buildIdx, "edit must keep the original order:\n%s", out)
	assert.Contains(t, out, "npm ci")
}

func parseFreshSetJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	payload := parseJSONObject(t, out)
	for _, key := range []string{"schema_version", "key", "action", "changed", "outcome"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("set JSON missing %q: %s", key, out)
		}
	}
	return payload
}

func parseJSONObject(t *testing.T, out string) map[string]any {
	t.Helper()
	start := strings.Index(out, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON object in:\n%s", out)
	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(out[start:]))
	require.NoError(t, dec.Decode(&payload), "parse JSON:\n%s", out)
	return payload
}
