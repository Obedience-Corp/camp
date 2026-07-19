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

func TestIntegration_WorkitemListFiltersNonTTYAndJSON(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-list-filters"
	_, err := tc.InitCampaign(dir, "workitem-list-filters", "product")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(dir, "workitem", "create", "auth-design", "--type", "design", "--title", "Auth Design")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workitem", "create", "search-notes", "--type", "explore", "--title", "Search Notes")
	require.NoError(t, err)

	text, err := tc.RunCampInDir(dir, "workitem", "list", "design")
	require.NoError(t, err, "compact list: %s", text)
	assert.Contains(t, text, "Auth Design")
	assert.NotContains(t, text, "Search Notes")

	raw, err := tc.RunCampInDir(dir, "workitem", "list", "active", "--json")
	require.NoError(t, err, "JSON list: %s", raw)
	start := strings.Index(raw, "{")
	require.GreaterOrEqual(t, start, 0, "missing JSON: %s", raw)
	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Items         []struct {
			AttentionStage string `json:"attention_stage"`
			LifecycleStage string `json:"lifecycle_stage"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw[start:]), &payload))
	assert.Equal(t, "workitems/v1alpha9", payload.SchemaVersion)
	require.NotEmpty(t, payload.Items)
	for _, item := range payload.Items {
		assert.True(t, item.AttentionStage == "active" || item.LifecycleStage == "active")
	}

	legacy, err := tc.RunCampInDir(dir, "workitem", "--list", "--type", "design")
	require.NoError(t, err, "legacy --list: %s", legacy)
	assert.Contains(t, legacy, "Auth Design")
}
