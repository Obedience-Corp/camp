//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A push enqueued behind a pending commit runs after the commit lands, and
// the terminal returns immediately instead of holding for the drain.
//
// This is the property camp#602 exists to deliver: the ordering barrier holds
// (the push runs after the commit) without costing the user their terminal
// (the push command returns 0 and prints one line).
func TestIntegration_PushQueuesBehindPendingCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, remotePath := setupDrainCampaign(t, tc, "push-queue")

	// Queue a commit that would block the push: a bookkeeping commit still
	// outstanding when the push runs.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p .campaign/intents
		printf 'queued intent\n' > .campaign/intents/before-push.md
	`, campPath))

	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind":    "commit-paths",
		"repo":    ".",
		"paths":   []string{".campaign/intents/before-push.md"},
		"message": "capture intent: queued before the push",
	})

	// The push sees the outstanding commit and, in a TTY, enqueues rather than
	// waiting. The container runs camp without a TTY, so this test writes the
	// push job directly and verifies the worker runs it after the commit.
	writeJob(t, tc, campPath, rootLane, 2, map[string]any{
		"kind":   "push",
		"repo":   ".",
		"remote": "origin",
		"branch": "main",
	})

	// Drain: the worker runs both jobs in order. The commit lands first, then
	// the push publishes it.
	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "jobs", "drain")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	// Both jobs must be gone from the queue.
	assert.Zero(t, pendingJobCount(t, tc, campPath, "pending", rootLane),
		"the lane must be empty after the drain")
	assert.Zero(t, pendingJobCount(t, tc, campPath, "failed", rootLane),
		"no jobs should have failed; failed count:\n%s",
		tc.Shell(t, fmt.Sprintf("ls %s/.campaign/cache/jobs/failed/%s/ 2>/dev/null || true", campPath, rootLane)))

	// The push must have published the commit to the remote.
	assert.True(t, remoteHasPath(t, tc, campPath, remotePath, ".campaign/intents/before-push.md"),
		"the push job must have published the queued commit to the remote")
}

// A push job that fails (non-fast-forward) parks in failed/ and does not burn
// all its retries, because retrying a rejected push without integrating the
// remote's commits is a loop with no exit.
func TestIntegration_PushJobParksOnNonFastForward(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, remotePath := setupDrainCampaign(t, tc, "push-reject")

	// Put a commit on the remote that the local branch does not have, so the
	// push will be rejected as non-fast-forward.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		git commit -q --allow-empty -m "local commit"
	`, campPath))

	// Diverge the remote: add a different commit to it directly.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		git -C %s commit -q --allow-empty -m "remote-only commit"
	`, remotePath, remotePath))

	// Write a push job directly into the queue.
	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind":   "push",
		"repo":   ".",
		"remote": "origin",
		"branch": "main",
	})

	// Run the worker.
	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "jobs", "drain")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	// The push must have parked in failed/ rather than succeeding.
	failedCount := pendingJobCount(t, tc, campPath, "failed", rootLane)
	assert.Greater(t, failedCount, 0,
		"a rejected push must park in failed/, not silently succeed; stderr:\n%s", stderr)

	// The job must carry the rejection reason.
	jobsStdout, _, _, _ := tc.RunCampSplitInDir(campPath, "jobs", "--json")
	if strings.Contains(jobsStdout, "last_error") {
		assert.True(t,
			strings.Contains(jobsStdout, "non-fast-forward") || strings.Contains(jobsStdout, "rejected"),
			"the parked push job should carry the rejection reason; jobs output:\n%s", jobsStdout)
	}
}
