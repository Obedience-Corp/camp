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

// TestIntentCrawl_TTYMovesInboxToReady drives a single intent crawl
// session through a real TTY, selecting "Move to another status"
// and then "ready" in the destination picker. It verifies the file
// moves to the ready directory and the source is gone.
//
// This is the smoke flow described by the design's
// 06-execution-workflow.md Step 6.
func TestIntentCrawl_TTYMovesInboxToReady(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignPath = "/campaigns/intent-crawl-tty"
	_, err := tc.InitCampaign(campaignPath, "intent-crawl-tty", "product")
	require.NoError(t, err)

	// Seed one inbox intent (non-interactive, agent path).
	_, err = tc.RunCampInDir(campaignPath, "intent", "add", "Crawl Me", "--no-commit")
	require.NoError(t, err)

	inboxLS, err := execLS(tc, campaignPath+"/.campaign/intents/inbox")
	require.NoError(t, err)
	files := strings.Fields(strings.TrimSpace(inboxLS))
	require.Len(t, files, 1, "expected exactly one seeded intent in inbox")
	intentFile := files[0]

	// The first-step menu has Move as the second option, so one
	// down-arrow moves the cursor onto Move. The destination picker
	// then opens; ready is the first live destination, so enter
	// selects it.
	moveStep := "\x1b[B\r" // arrow down + enter (select Move)
	pickReady := "\r"      // first option in destination picker is "ready"

	output, err := tc.RunCampInteractiveStepsInDir(
		campaignPath,
		[]InteractiveStep{
			{WaitFor: "Intent 1/1", Input: moveStep},
			{WaitFor: "Destinations", Input: pickReady},
			{WaitFor: "Intent crawl complete", Input: ""},
		},
		"intent", "crawl", "--status", "inbox", "--limit", "1", "--no-commit",
	)
	require.NoError(t, err, "intent crawl session failed; output:\n%s", output)
	assert.Contains(t, output, "Moved to ready: 1", "summary should report one move to ready")

	// Source file in inbox/ is gone, and a new file with the same
	// name appears under ready/.
	exists, err := tc.CheckDirExists(campaignPath + "/.campaign/intents/inbox/" + intentFile)
	require.NoError(t, err)
	assert.False(t, exists, "source intent file should no longer exist in inbox")

	readyContent, err := tc.ReadFile(campaignPath + "/.campaign/intents/ready/" + intentFile)
	require.NoError(t, err, "intent should now exist under ready/")
	assert.Contains(t, readyContent, "status: ready", "frontmatter should reflect new status")
}

// TestIntentCrawl_RejectsDungeonStatusFilter verifies that
// `--status archived` (and similar dungeon spellings) fail with a
// clear error rather than silently skipping.
func TestIntentCrawl_RejectsDungeonStatusFilter(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignPath = "/campaigns/intent-crawl-reject"
	_, err := tc.InitCampaign(campaignPath, "intent-crawl-reject", "product")
	require.NoError(t, err)

	for _, bad := range []string{"archived", "dungeon/archived", "done"} {
		out, err := tc.RunCampInDir(campaignPath, "intent", "crawl", "--status", bad)
		assert.Error(t, err, "--status %q should fail", bad)
		assert.Contains(t, out, "live", "error should mention live statuses (got: %s)", out)
	}
}

// TestManifest_IntentCrawlAgentRestricted verifies that the new
// command appears in the manifest with agent_allowed=false and
// interactive=true.
func TestManifest_IntentCrawlAgentRestricted(t *testing.T) {
	tc := GetSharedContainer(t)

	output, err := tc.RunCamp("__manifest")
	require.NoError(t, err)

	var manifest struct {
		Commands []struct {
			Path         string `json:"path"`
			AgentAllowed bool   `json:"agent_allowed"`
			Interactive  bool   `json:"interactive"`
		} `json:"commands"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &manifest), "manifest should be valid JSON")

	var found bool
	for _, c := range manifest.Commands {
		if c.Path == "intent crawl" {
			found = true
			assert.False(t, c.AgentAllowed, "intent crawl should be agent_allowed=false")
			assert.True(t, c.Interactive, "intent crawl should be interactive=true")
		}
	}
	assert.True(t, found, "intent crawl missing from manifest")
}
