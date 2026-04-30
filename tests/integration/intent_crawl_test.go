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

// TestIntentCrawl_AutoCommitMoveCommitsScopedFiles drives a crawl
// session WITHOUT --no-commit and verifies the auto-commit:
//   - is created
//   - includes the destination file (status=A)
//   - includes the source deletion (status=D)
//   - includes .intents.jsonl and crawl.jsonl
//   - does NOT include unrelated dirty files
//
// This is the regression test for the path-normalization bug where
// production absolute paths from IntentService were dropped by
// BuildCommitPaths' relative-only filter, leaving auto-commit with
// an empty selective scope.
func TestIntentCrawl_AutoCommitMoveCommitsScopedFiles(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignPath = "/campaigns/intent-crawl-autocommit"
	_, err := tc.InitCampaign(campaignPath, "intent-crawl-autocommit", "product")
	require.NoError(t, err)

	// Seed and commit one inbox intent so the source file is
	// tracked. `camp intent add` auto-commits by default.
	_, err = tc.RunCampInDir(campaignPath, "intent", "add", "Auto Commit Me")
	require.NoError(t, err)

	inboxLS, err := execLS(tc, campaignPath+"/.campaign/intents/inbox")
	require.NoError(t, err)
	files := strings.Fields(strings.TrimSpace(inboxLS))
	require.Len(t, files, 1, "expected exactly one seeded intent in inbox")
	intentFile := files[0]

	// Drop an unrelated dirty file so the test can prove auto-commit
	// scopes only intent crawl owned changes.
	require.NoError(t, tc.WriteFile(campaignPath+"/unrelated-dirty.txt", "should not be committed\n"))

	preCrawlHead := tc.GitOutput(t, campaignPath, "rev-parse", "HEAD")

	moveStep := "\x1b[B\r" // arrow down + enter (Move)
	pickReady := "\r"      // first option in destination picker is "ready"

	output, err := tc.RunCampInteractiveStepsInDir(
		campaignPath,
		[]InteractiveStep{
			{WaitFor: "Intent 1/1", Input: moveStep},
			{WaitFor: "Destinations", Input: pickReady},
			{WaitFor: "Committed changes to git", Input: ""},
		},
		"intent", "crawl", "--status", "inbox", "--limit", "1",
	)
	require.NoError(t, err, "intent crawl auto-commit session failed; output:\n%s", output)
	assert.Contains(t, output, "Moved to ready: 1")
	assert.Contains(t, output, "Committed changes to git",
		"summary should report a successful auto-commit")

	postCrawlHead := tc.GitOutput(t, campaignPath, "rev-parse", "HEAD")
	require.NotEqual(t, preCrawlHead, postCrawlHead, "auto-commit should advance HEAD")

	subject := tc.GitOutput(t, campaignPath, "log", "-1", "--pretty=%s")
	assert.Contains(t, subject, "Crawl: intent crawl completed",
		"auto-commit subject should describe intent crawl")

	diff := tc.GitOutput(t, campaignPath, "diff-tree", "--no-commit-id",
		"--name-status", "--no-renames", "-r", "HEAD")
	assert.Contains(t, diff, "A\t.campaign/intents/ready/"+intentFile,
		"destination file should be added")
	assert.Contains(t, diff, "D\t.campaign/intents/inbox/"+intentFile,
		"source file should be deleted")
	assert.Contains(t, diff, ".campaign/intents/crawl.jsonl",
		"crawl log should be in the commit")
	assert.Contains(t, diff, ".campaign/intents/.intents.jsonl",
		"audit log should be in the commit")
	assert.NotContains(t, diff, "unrelated-dirty.txt",
		"unrelated dirty file must not be in the commit")

	// Sanity: unrelated dirty file is still dirty in the working tree.
	statusOut := tc.GitOutput(t, campaignPath, "status", "--porcelain", "unrelated-dirty.txt")
	assert.Contains(t, statusOut, "unrelated-dirty.txt",
		"unrelated dirty file should remain uncommitted")
}

// TestIntentCrawl_KeepOnlyAutoCommitsCrawlLog verifies that a
// session with only keep/skip decisions still auto-commits the
// crawl.jsonl entries it appended. Previously the auto-commit was
// gated on HasMoves(), which left the crawl log dirty.
func TestIntentCrawl_KeepOnlyAutoCommitsCrawlLog(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignPath = "/campaigns/intent-crawl-keeponly"
	_, err := tc.InitCampaign(campaignPath, "intent-crawl-keeponly", "product")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(campaignPath, "intent", "add", "Keep Me")
	require.NoError(t, err)

	preHead := tc.GitOutput(t, campaignPath, "rev-parse", "HEAD")

	keepStep := "\r" // first option is Keep

	output, err := tc.RunCampInteractiveStepsInDir(
		campaignPath,
		[]InteractiveStep{
			{WaitFor: "Intent 1/1", Input: keepStep},
			{WaitFor: "Committed changes to git", Input: ""},
		},
		"intent", "crawl", "--status", "inbox", "--limit", "1",
	)
	require.NoError(t, err, "keep-only crawl session failed; output:\n%s", output)
	assert.Contains(t, output, "Kept:    1")

	postHead := tc.GitOutput(t, campaignPath, "rev-parse", "HEAD")
	require.NotEqual(t, preHead, postHead, "keep-only session should still create a commit for crawl.jsonl")

	diff := tc.GitOutput(t, campaignPath, "diff-tree", "--no-commit-id",
		"--name-status", "--no-renames", "-r", "HEAD")
	assert.Contains(t, diff, ".campaign/intents/crawl.jsonl",
		"crawl log entry from keep should be committed")
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
