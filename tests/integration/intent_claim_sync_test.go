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

// TestIntentSync_MovesMergedPRToDungeonDone drives the full claim -> sync
// loop against the real camp binary: claim two intents with different
// tracked PRs, point PATH at a fake gh that reports one MERGED and one
// CLOSED, and verify camp intent sync only auto-moves the merged one (after
// first confirming --dry-run moves nothing).
func TestIntentSync_MovesMergedPRToDungeonDone(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/intent-sync-merge"

	_, err := tc.InitCampaign(path, "intent-sync-merge", "product")
	require.NoError(t, err)

	mergedID := createIntentForSync(t, tc, path, "Merged feature")
	closedID := createIntentForSync(t, tc, path, "Closed feature")

	const mergedPRURL = "https://github.com/Obedience-Corp/camp/pull/9001"
	const closedPRURL = "https://github.com/Obedience-Corp/camp/pull/9002"

	_, err = tc.RunCampInDir(path, "intent", "claim", mergedID, "--agent", "sync-test-agent", "--ref", mergedPRURL)
	require.NoError(t, err, "claim merged intent")
	_, err = tc.RunCampInDir(path, "intent", "claim", closedID, "--agent", "sync-test-agent", "--ref", closedPRURL)
	require.NoError(t, err, "claim closed intent")

	ghDir := installFakeGH(t, tc, path, map[string]string{
		mergedPRURL: `{"state":"MERGED"}`,
		closedPRURL: `{"state":"CLOSED"}`,
	})

	// Dry run first: nothing should move yet, but the merge should be reported.
	dryOut, err := runCampWithPath(tc, path, ghDir, "intent", "sync", "--dry-run")
	require.NoError(t, err, "sync --dry-run: %s", dryOut)
	assert.Contains(t, dryOut, "Would move to dungeon/done")
	assert.Contains(t, dryOut, mergedPRURL)

	mergedStillInbox, err := tc.CheckFileExists(fmt.Sprintf("%s/.campaign/intents/inbox/%s.md", path, mergedID))
	require.NoError(t, err)
	assert.True(t, mergedStillInbox, "dry-run must not move the merged intent")

	// Real run: the merged PR auto-closes, the closed-unmerged PR is only reported.
	out, err := runCampWithPath(tc, path, ghDir, "intent", "sync")
	require.NoError(t, err, "sync: %s", out)
	assert.Contains(t, out, "Moved to dungeon/done")
	assert.Contains(t, out, mergedPRURL)
	assert.Contains(t, out, "PR closed without merging, not auto-moved")
	assert.Contains(t, out, closedPRURL)

	donePath := fmt.Sprintf("%s/.campaign/intents/dungeon/done/%s.md", path, mergedID)
	doneExists, err := tc.CheckFileExists(donePath)
	require.NoError(t, err)
	assert.True(t, doneExists, "merged intent should have moved to dungeon/done")

	doneContent, err := tc.ReadFile(donePath)
	require.NoError(t, err)
	assert.Contains(t, doneContent, "status: dungeon/done")
	assert.Contains(t, doneContent, "PR merged: "+mergedPRURL)

	inboxGone, err := tc.CheckFileExists(fmt.Sprintf("%s/.campaign/intents/inbox/%s.md", path, mergedID))
	require.NoError(t, err)
	assert.False(t, inboxGone, "merged intent should no longer be in inbox")

	closedStillInbox, err := tc.CheckFileExists(fmt.Sprintf("%s/.campaign/intents/inbox/%s.md", path, closedID))
	require.NoError(t, err)
	assert.True(t, closedStillInbox, "closed-unmerged intent must never be auto-moved")

	auditContent, err := tc.ReadFile(path + "/.campaign/intents/.intents.jsonl")
	require.NoError(t, err)
	assert.Contains(t, auditContent, `"event":"sync"`)
	assert.Contains(t, auditContent, mergedID)
}

// TestIntentSync_RequiresGH verifies the "gh missing" boundary: sync must
// fail with a clear, actionable error when an intent has a tracked PR but gh
// is not on PATH, rather than an opaque exec failure.
func TestIntentSync_RequiresGH(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/intent-sync-no-gh"

	_, err := tc.InitCampaign(path, "intent-sync-no-gh", "product")
	require.NoError(t, err)

	id := createIntentForSync(t, tc, path, "Needs a PR check")
	_, err = tc.RunCampInDir(path, "intent", "claim", id, "--agent", "sync-test-agent",
		"--ref", "https://github.com/Obedience-Corp/camp/pull/1")
	require.NoError(t, err)

	out, err := runCampWithPath(tc, path, "/nonexistent-empty-path", "intent", "sync")
	require.Error(t, err, "sync without gh on PATH should fail: %s", out)
	assert.Contains(t, out, "gh CLI not found in PATH")
}

// TestIntentClaimRelease_RoundTrip exercises claim, a second claim adding a PR
// ref, then release, verifying frontmatter and the audit trail at each step.
func TestIntentClaimRelease_RoundTrip(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/intent-claim-release"

	_, err := tc.InitCampaign(path, "intent-claim-release", "product")
	require.NoError(t, err)

	id := createIntentForSync(t, tc, path, "Claim me")

	claimOut, err := tc.RunCampInDir(path, "intent", "claim", id, "--agent", "session-1")
	require.NoError(t, err, "claim: %s", claimOut)
	assert.Contains(t, claimOut, "claimed by session-1")

	intentPath := fmt.Sprintf("%s/.campaign/intents/inbox/%s.md", path, id)
	claimedContent, err := tc.ReadFile(intentPath)
	require.NoError(t, err)
	assert.Contains(t, claimedContent, "assigned_to: session-1")
	assert.Contains(t, claimedContent, "assigned_at:")

	prURL := "https://github.com/Obedience-Corp/camp/pull/555"
	reclaimOut, err := tc.RunCampInDir(path, "intent", "claim", id, "--agent", "session-1", "--ref", prURL)
	require.NoError(t, err, "reclaim with ref: %s", reclaimOut)

	withRefContent, err := tc.ReadFile(intentPath)
	require.NoError(t, err)
	assert.Contains(t, withRefContent, prURL)

	releaseOut, err := tc.RunCampInDir(path, "intent", "release", id)
	require.NoError(t, err, "release: %s", releaseOut)
	assert.Contains(t, releaseOut, "released (was claimed by session-1)")

	releasedContent, err := tc.ReadFile(intentPath)
	require.NoError(t, err)
	assert.NotContains(t, releasedContent, "assigned_to: session-1")
	assert.Contains(t, releasedContent, prURL, "work_ref must survive release for sync to still resolve it")

	auditContent, err := tc.ReadFile(path + "/.campaign/intents/.intents.jsonl")
	require.NoError(t, err)
	assert.Contains(t, auditContent, `"event":"claim"`)
	assert.Contains(t, auditContent, `"event":"release"`)
}

// createIntentForSync creates an intent via the real CLI and returns its id,
// parsed from the --json envelope (the same idiom other integration tests use
// for workitem create --json).
func createIntentForSync(t *testing.T, tc *TestContainer, path, title string) string {
	t.Helper()

	out, err := tc.RunCampInDir(path, "intent", "add", title, "--json", "--no-commit")
	require.NoError(t, err, "intent add --json: %s", out)

	start := strings.Index(out, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON in output: %s", out)

	var payload struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(out[start:]), &payload), "parse: %s", out)
	require.NotEmpty(t, payload.ID, "intent add did not return an id: %s", out)
	return payload.ID
}

// installFakeGH writes an executable gh script under path that answers
// `gh pr view <url> --json state` from responses, or a nonzero exit with
// stderr for any url not in the table. Installed inside the campaign under
// test (not a shared container path) so it never leaks into other tests that
// share the pooled container.
func installFakeGH(t *testing.T, tc *TestContainer, path string, responses map[string]string) string {
	t.Helper()

	ghDir := path + "/.ghfake"

	var script strings.Builder
	script.WriteString("#!/bin/sh\ncase \"$3\" in\n")
	for url, body := range responses {
		script.WriteString(fmt.Sprintf("  '%s') echo '%s'; exit 0 ;;\n", url, body))
	}
	script.WriteString("  *) echo \"fake gh: no fixture for $3\" >&2; exit 1 ;;\nesac\n")

	require.NoError(t, tc.WriteFile(ghDir+"/gh", script.String()))
	_, exitCode, err := tc.ExecCommand("chmod", "+x", ghDir+"/gh")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	return ghDir
}

// runCampWithPath runs camp from dir like RunCampInDir, but prepends
// extraPathDir onto PATH first so a test-scoped fake binary (gh) is found
// without ever installing into the shared container's real PATH.
func runCampWithPath(tc *TestContainer, dir, extraPathDir string, args ...string) (string, error) {
	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		quotedArgs[i] = "'" + escaped + "'"
	}
	cmdStr := fmt.Sprintf("cd %s && PATH=%s:$PATH /camp %s 2>&1", dir, extraPathDir, strings.Join(quotedArgs, " "))

	output, exitCode, err := tc.ExecCommand("sh", "-c", cmdStr)
	if err != nil {
		return output, fmt.Errorf("failed to execute camp: %w", err)
	}
	if exitCode != 0 {
		return output, fmt.Errorf("camp exited with code %d: %s", exitCode, output)
	}
	return output, nil
}
