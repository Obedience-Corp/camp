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

func createExploreWorkitem(t *testing.T, tc *TestContainer, campaign, slug, title, body string) {
	t.Helper()
	_, err := tc.RunCampInDir(campaign, "workitem", "create", slug, "--type", "explore", "--title", title)
	require.NoError(t, err, "create explore workitem %s", slug)
	require.NoError(t, tc.WriteFile(campaign+"/workflow/explore/"+slug+"/README.md", body))
}

// TestGatherExplore_MovesSourcesIntoGatheredPackage proves the gather engine
// generalizes to a second directory-based workitem type by registration
// alone: `camp gather explore` reuses every planning, execution, and
// bookkeeping path already exercised for `camp gather design`.
func TestGatherExplore_MovesSourcesIntoGatheredPackage(t *testing.T) {
	tc := GetSharedContainer(t)
	campaign := "/campaigns/gather-explore"

	_, err := tc.InitCampaign(campaign, "gather-explore", "product")
	require.NoError(t, err)

	createExploreWorkitem(t, tc, campaign, "spike-cache", "Cache Spike", "# Cache Spike\n\nExploring cache eviction.\n")
	createExploreWorkitem(t, tc, campaign, "spike-queue", "Queue Spike", "# Queue Spike\n\nExploring queue backpressure.\n")
	tc.GitOutput(t, campaign, "add", "-A")
	tc.GitOutput(t, campaign, "commit", "-m", "seed explore workitems")

	output, err := tc.RunCampInDir(campaign, "gather", "explore", "spike-cache", "spike-queue", "--title", "Unified Spikes", "--json")
	require.NoError(t, err, "gather output: %s", output)

	var result struct {
		Gathered struct {
			ID           string `json:"id"`
			Type         string `json:"type"`
			RelativePath string `json:"relative_path"`
		} `json:"gathered"`
		Sources []struct {
			Slug string `json:"slug"`
		} `json:"sources"`
		Committed bool     `json:"committed"`
		Warnings  []string `json:"warnings"`
	}
	jsonStart := strings.Index(output, "{")
	require.GreaterOrEqual(t, jsonStart, 0, "no JSON in output: %s", output)
	require.NoError(t, json.Unmarshal([]byte(output[jsonStart:]), &result), "output: %s", output)

	assert.Equal(t, "explore", result.Gathered.Type)
	assert.Equal(t, "workflow/explore/unified-spikes", result.Gathered.RelativePath)
	assert.Len(t, result.Sources, 2)
	assert.True(t, result.Committed, "gather should auto-commit")
	assert.Empty(t, result.Warnings, "gather should complete without warnings")

	for _, slug := range []string{"spike-cache", "spike-queue"} {
		moved, err := tc.CheckDirExists(campaign + "/workflow/explore/unified-spikes/" + slug)
		require.NoError(t, err)
		assert.True(t, moved, "%s should live inside the gathered package", slug)

		old, err := tc.CheckDirExists(campaign + "/workflow/explore/" + slug)
		require.NoError(t, err)
		assert.False(t, old, "%s should no longer exist at the top level", slug)
	}

	marker, err := tc.ReadFile(campaign + "/workflow/explore/unified-spikes/spike-cache/.workitem")
	require.NoError(t, err)
	assert.Contains(t, marker, "gathered_into: "+result.Gathered.ID)
	assert.Contains(t, marker, "gathered_at:")
	assert.Contains(t, marker, "version: v1alpha8")

	listOutput, err := tc.RunCampInDir(campaign, "workitem", "--json", "--type", "explore")
	require.NoError(t, err)
	assert.Contains(t, listOutput, "workflow/explore/unified-spikes")
	assert.NotContains(t, listOutput, `"relative_path": "workflow/explore/spike-cache"`)
	assert.NotContains(t, listOutput, `"relative_path": "workflow/explore/spike-queue"`)

	gitStatus := tc.GitOutput(t, campaign, "status", "--porcelain")
	assert.Empty(t, strings.TrimSpace(gitStatus), "working tree should be clean after gather commit")
}
