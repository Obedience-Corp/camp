//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const questSentinelDate = "2000-01-01T00:00:00Z"

const sentinelQuestYAML = `id: qst_default
name: default
purpose: Default working context for this campaign
description: Fallback quest for work that doesn't belong to a specific quest.
status: open
created_at: "2000-01-01T00:00:00Z"
updated_at: "2000-01-01T00:00:00Z"
`

func TestQuestDefault_FreshInitHasRealTimestamp(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/quest-ts-fresh"
	_, err := tc.InitCampaign(path, "quest-ts-fresh", "product")
	require.NoError(t, err)

	content, err := tc.ReadFile(path + "/.campaign/quests/default/quest.yaml")
	require.NoError(t, err)
	assert.NotContains(t, content, questSentinelDate, "fresh default quest must not carry the sentinel date")
	assert.NotContains(t, content, "<no value>", "timestamp template var must render")

	out, err := tc.RunCampInDir(path, "quest", "list")
	require.NoError(t, err, out)
	assert.Contains(t, out, "default", "default quest must load and list")
}

func TestQuestDefault_RepairBackfillsSentinelAndCommits(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/quest-ts-repair"
	_, err := tc.InitCampaign(path, "quest-ts-repair", "product")
	require.NoError(t, err)

	qf := path + "/.campaign/quests/default/quest.yaml"
	require.NoError(t, tc.WriteFile(qf, sentinelQuestYAML))
	tc.GitOutput(t, path, "add", "-A")
	tc.GitOutput(t, path, "commit", "-m", "inject sentinel")

	out, err := tc.RunCampInDir(path, "init", path, "--repair", "--yes")
	require.NoError(t, err, out)

	after, err := tc.ReadFile(qf)
	require.NoError(t, err)
	assert.NotContains(t, after, questSentinelDate, "repair must backfill the sentinel date")

	status := tc.GitOutput(t, path, "status", "--porcelain", ".campaign/quests/default/quest.yaml")
	assert.Empty(t, status, "repair must commit the backfilled quest.yaml, left dirty: %q", status)

	out2, err := tc.RunCampInDir(path, "init", path, "--repair", "--yes")
	require.NoError(t, err, out2)
	assert.NotContains(t, out2, "backfill default quest sentinel date", "second repair must be a no-op")
}

func TestQuestList_SkipsStrayDirWithoutWarning(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/quest-stray"
	_, err := tc.InitCampaign(path, "quest-stray", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(path+"/.campaign/quests/20260608-test/ui-state.json", `{"ui":true}`))
	require.NoError(t, tc.WriteFile(path+"/.campaign/quests/broken/quest.yaml", ":\n"))

	_, exitCode, err := tc.ExecCommand("sh", "-c", "cd "+path+" && /camp quest list 1>/dev/null 2>/tmp/qe")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	stderr, _, _ := tc.ExecCommand("cat", "/tmp/qe")

	assert.NotContains(t, stderr, "20260608-test", "stray dir without quest.yaml must not warn")
	assert.Contains(t, stderr, `unreadable quest "broken"`, "malformed quest.yaml must still warn")
}

func TestReadme_RendersCampaignVars(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/readme-render"
	_, err := tc.InitCampaign(path, "readme-render", "product")
	require.NoError(t, err)

	readme, err := tc.ReadFile(path + "/README.md")
	require.NoError(t, err)
	assert.NotContains(t, readme, "<no value>", "README template vars must render through the engine")
	assert.Contains(t, readme, "# readme-render", "README must render the campaign name")
}
