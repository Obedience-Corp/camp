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

func TestIntegration_WorkitemListTagAndProjectFilters(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-list-tag-project"
	_, err := tc.InitCampaign(dir, "workitem-list-tag-project", "product")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(dir, "workitem", "create", "tagboth", "--type", "design",
		"--title", "TagBoth", "--tag", "public-launch", "--tag", "schema")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workitem", "create", "tagone", "--type", "design",
		"--title", "TagOne", "--tag", "public-launch")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workitem", "create", "projcamp", "--type", "design",
		"--title", "ProjCamp", "--project", "projects/camp")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workitem", "create", "projfest", "--type", "design",
		"--title", "ProjFest", "--project", "projects/fest")
	require.NoError(t, err)

	andText, err := tc.RunCampInDir(dir, "workitem", "list", "--tag", "public-launch", "--tag", "schema")
	require.NoError(t, err, "tag AND list: %s", andText)
	assert.Contains(t, andText, "TagBoth", "item carrying both tags must be listed")
	assert.NotContains(t, andText, "TagOne", "--tag is AND: an item missing one named tag must be excluded")
	assert.NotContains(t, andText, "ProjCamp", "an untagged item must be excluded by a tag filter")

	orText, err := tc.RunCampInDir(dir, "workitem", "list", "--project", "projects/camp", "--project", "projects/fest")
	require.NoError(t, err, "project OR list: %s", orText)
	assert.Contains(t, orText, "ProjCamp", "--project is OR: an item touching either project must be listed")
	assert.Contains(t, orText, "ProjFest", "--project is OR: an item touching either project must be listed")
	assert.NotContains(t, orText, "TagBoth", "an item touching neither project must be excluded")

	parityTag, err := tc.RunCampInDir(dir, "workitem", "list", "--tag", "Public-Launch")
	require.NoError(t, err, "unnormalized --tag list: %s", parityTag)
	assert.Contains(t, parityTag, "TagBoth", "--tag Public-Launch must normalize and match stored public-launch")
	assert.Contains(t, parityTag, "TagOne", "--tag Public-Launch must normalize and match stored public-launch")

	parityProject, err := tc.RunCampInDir(dir, "workitem", "list", "--project", "projects/camp/")
	require.NoError(t, err, "trailing-slash --project list: %s", parityProject)
	assert.Contains(t, parityProject, "ProjCamp", "--project projects/camp/ must normalize and match stored projects/camp")

	emptyProject, err := tc.RunCampInDir(dir, "workitem", "list", "--project", "/")
	require.Error(t, err, "--project / must fail as empty after normalization: %s", emptyProject)
}
