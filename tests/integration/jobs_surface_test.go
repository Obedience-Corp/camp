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

// The queue's user surface. These run the real binary because the properties
// are about what a person sees and what survives on disk afterward, neither of
// which a package test can observe.

// writeFailedJob puts a job straight into a lane's failed/ directory.
func writeFailedJob(t *testing.T, tc *TestContainer, campPath, laneSlug string, seq int, doc map[string]any) {
	t.Helper()
	doc["seq"] = seq
	if _, ok := doc["id"]; !ok {
		doc["id"] = fmt.Sprintf("job-test-%04d", seq)
	}
	if _, ok := doc["created_at"]; !ok {
		doc["created_at"] = "2026-07-28T00:00:00.000Z"
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	require.NoError(t, err)

	dir := fmt.Sprintf("%s/.campaign/cache/jobs/failed/%s", campPath, laneSlug)
	tc.Shell(t, fmt.Sprintf("mkdir -p %s", dir))
	require.NoError(t, tc.WriteFile(fmt.Sprintf("%s/%07d.json", dir, seq), string(data)))
}

// Deferred criterion 5: a failed job announces itself on every foreground
// command until it is resolved.
//
// A notice that appeared once and then stopped would be the same as no notice
// for anyone not watching that terminal, and a failed job is work camp promised
// to do and did not.
func TestIntegration_FailedJobNoticeRepeatsUntilResolved(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "jobs-notice")

	writeFailedJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind": "commit-paths", "repo": ".",
		"paths": []string{"gone.md"}, "message": "capture intent: gone",
		"attempts": 3,
	})

	// Two unrelated commands, both of which must carry the notice. The second
	// is the point: the notice is not a one-shot.
	for _, args := range [][]string{{"status", "--short"}, {"list"}} {
		_, stderr, _, err := tc.RunCampSplitInDir(campPath, args...)
		require.NoError(t, err)
		assert.Contains(t, stderr, "1 deferred commit failed",
			"%v must carry the failed-job notice; stderr:\n%s", args, stderr)
		assert.Contains(t, stderr, "camp jobs",
			"the notice must name where to look; stderr:\n%s", stderr)
	}

	// Resolving it clears the notice, so the line means something.
	_, _, exitCode, err := tc.RunCampSplitInDir(campPath, "jobs", "drop", "all")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	_, stderr, _, err := tc.RunCampSplitInDir(campPath, "status", "--short")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "deferred commit failed",
		"the notice must clear once nothing is failed; stderr:\n%s", stderr)
}

// The notice must not appear inside `camp jobs` itself, where it would restate
// what the user is already looking at, nor under --json, where an extra line is
// a broken parse rather than a warning.
func TestIntegration_FailedJobNoticeStaysOutOfJobsAndJSON(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "jobs-notice-suppressed")

	writeFailedJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind": "commit-paths", "repo": ".",
		"paths": []string{"gone.md"}, "message": "capture intent: gone",
		"attempts": 3,
	})

	_, stderr, _, err := tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "deferred commit failed",
		"camp jobs already shows the failure; stderr:\n%s", stderr)

	stdout, stderr, _, err := tc.RunCampSplitInDir(campPath, "jobs", "--json")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "deferred commit failed")
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc),
		"stdout must be exactly one JSON document; got:\n%s", stdout)
}

// Criterion 9's safer default: dropping a job discards camp's promise to commit
// something, never the something. The file stays on disk and the next ordinary
// commit picks it up.
func TestIntegration_JobsDropKeepsContentForTheNextCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "jobs-drop-keeps")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p .campaign/intents
		printf 'work the user did\n' > .campaign/intents/dropped.md
	`, campPath))

	writeFailedJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind": "commit-paths", "repo": ".",
		"paths": []string{".campaign/intents/dropped.md"},
		"message": "capture intent: dropped", "attempts": 3,
	})

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "jobs", "drop", "all")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stdout+stderr, "still in your working tree",
		"drop must say what it did not discard; output:\n%s", stdout+stderr)

	exists, err := tc.CheckFileExists(campPath + "/.campaign/intents/dropped.md")
	require.NoError(t, err)
	assert.True(t, exists, "drop must not delete the content the job was going to commit")

	// The next ordinary commit sweeps it up, which is what makes drop safe.
	_, stderr, exitCode, err = tc.RunCampSplitInDir(campPath, "commit", "-m", "pick up dropped work")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	tracked := tc.GitOutput(t, campPath, "ls-files", ".campaign/intents/dropped.md")
	assert.Contains(t, tracked, "dropped.md",
		"the dropped job's content must be committable by an ordinary commit")
}

// Retry re-runs a failed job to success once its cause is gone.
func TestIntegration_JobsRetryRunsTheJobToSuccess(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "jobs-retry")

	// The job failed because its path did not exist. Creating it is the user
	// fixing the cause, which is exactly what retry is for.
	writeFailedJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind": "commit-paths", "repo": ".",
		"paths": []string{".campaign/intents/retried.md"},
		"message": "capture intent: retried", "attempts": 3,
	})
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p .campaign/intents
		printf 'now it exists\n' > .campaign/intents/retried.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "jobs", "retry", "all")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	// A drain waits for the worker retry spawned, so the assertion does not
	// race the detached process.
	_, stderr, exitCode, err = tc.RunCampSplitInDir(campPath, "jobs", "drain")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	log := tc.GitOutput(t, campPath, "log", "--oneline", "-5")
	assert.Contains(t, log, "capture intent: retried",
		"a retried job must actually run; git log:\n%s", log)
	assert.Zero(t, pendingJobCount(t, tc, campPath, "failed", rootLane),
		"a job that succeeded on retry must not stay in failed/")
}

// The listing is what a user reads to decide what to do, so every state it
// shows names the command that resolves it.
func TestIntegration_JobsListingIsActionable(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "jobs-listing")

	writeFailedJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind": "commit-paths", "repo": ".",
		"paths": []string{"gone.md"}, "message": "capture intent: listed",
		"attempts": 3,
	})

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	assert.Contains(t, stdout, "capture intent: listed", "the row must name the work")
	assert.Contains(t, stdout, "camp jobs retry", "a failure must name how to retry it")
	assert.Contains(t, stdout, "camp jobs drop", "a failure must name how to give up on it")
	assert.Contains(t, stdout, "gave up after 3 attempts",
		"a parked job reports what it used, not a run that will never happen")
	assert.NotContains(t, stdout, "attempt 4 of 3",
		"a failed job must not describe an attempt past the bound; got:\n%s", stdout)
}

// --json is what an agent reads. Empty collections must be arrays, asserted on
// raw bytes because unmarshaling hides the difference and that is exactly how
// this class of bug reaches consumers.
func TestIntegration_JobsJSONShape(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "jobs-json")

	stdout, _, exitCode, err := tc.RunCampSplitInDir(campPath, "jobs", "--json")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, `"jobs": []`,
		"an empty queue must serialize jobs as [], never null; got:\n%s", stdout)

	// The lane directory, not the repo field, is what a listing reports, so
	// the job goes in the lane its repo slugs to.
	writeFailedJob(t, tc, campPath, "projects%2Fcamp", 1, map[string]any{
		"kind": "commit-paths", "class": "manifest", "repo": "projects/camp",
		"paths": []string{".campaign/manifests/v.json"}, "attempts": 3,
		"id": "job-20260728T000000Z-aa11",
	})

	require.NoError(t, tc.WriteFile("/tmp/jobs.json", ""))
	stdout, _, exitCode, err = tc.RunCampSplitInDir(campPath, "jobs", "--json")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	require.NoError(t, tc.WriteFile("/tmp/jobs.json", stdout))

	// Proved parseable by jq reading a file, not by a pipeline whose exit code
	// would come from jq rather than from camp.
	out := tc.Shell(t, "jq -r '.jobs[0] | .state, .lane, .class, .id' /tmp/jobs.json")
	lines := strings.Fields(strings.TrimSpace(out))
	require.Len(t, lines, 4, "jq output:\n%s\ndocument:\n%s", out, stdout)
	assert.Equal(t, "failed", lines[0])
	assert.Equal(t, "projects/camp", lines[1], "the lane must decode back to its repo path")
	assert.Equal(t, "manifest", lines[2])
	assert.Equal(t, "job-20260728T000000Z-aa11", lines[3])
}

// The doctor check is where the queue's quiet failures become findable without
// the user knowing the queue exists.
func TestIntegration_DoctorFlagsFailedAndStuckJobs(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "jobs-doctor")

	writeFailedJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind": "commit-paths", "repo": ".",
		"paths": []string{"gone.md"}, "message": "capture intent: doctored",
		"attempts": 3,
	})
	// A running job with no lane lock: claimed, but nobody is on it.
	runningDir := fmt.Sprintf("%s/.campaign/cache/jobs/running/%s", campPath, rootLane)
	tc.Shell(t, fmt.Sprintf("mkdir -p %s", runningDir))
	require.NoError(t, tc.WriteFile(runningDir+"/0000002.json", `{
  "id": "job-20260728T000000Z-bb22",
  "seq": 2,
  "kind": "commit-paths",
  "repo": ".",
  "paths": ["stalled.md"],
  "message": "capture intent: stalled",
  "created_at": "2026-07-28T00:00:00.000Z"
}`))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "doctor", "-c", "jobs")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "a failed job is an error-level finding")

	combined := stdout + stderr
	assert.Contains(t, combined, "deferred commit failed",
		"doctor must report the failed job; output:\n%s", combined)
	assert.Contains(t, combined, "no worker is running it",
		"doctor must report the stuck job; output:\n%s", combined)
	// The stray-colon regression: root-level issues have no submodule.
	assert.NotContains(t, combined, "✗ :",
		"a root-level issue must not render an empty location; output:\n%s", combined)
}

// Deferred criterion 9: deleting .campaign/cache mid-queue loses the jobs but
// never the content, and doctor says which files are now waiting on an ordinary
// commit.
func TestIntegration_DoctorReportsBookkeepingLostWithTheCache(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "jobs-doctor-lost")

	// A campaign with history, then an intent queued, then the cache deleted.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p .campaign/intents
		printf 'first\n' > .campaign/intents/committed.md
	`, campPath))
	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "-m", "establish history")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'queued then orphaned\n' > .campaign/intents/orphaned.md
	`, campPath))
	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind": "commit-paths", "repo": ".",
		"paths": []string{".campaign/intents/orphaned.md"},
		"message": "capture intent: orphaned",
	})
	tc.Shell(t, fmt.Sprintf("rm -rf %s/.campaign/cache", campPath))

	stdout, stderr, _, err := tc.RunCampSplitInDir(campPath, "doctor", "-c", "jobs")
	require.NoError(t, err)

	combined := stdout + stderr
	assert.Contains(t, combined, ".campaign/intents/orphaned.md",
		"doctor must name the file left behind; output:\n%s", combined)
	assert.Contains(t, combined, "camp commit",
		"doctor must name the recovery; output:\n%s", combined)

	exists, err := tc.CheckFileExists(campPath + "/.campaign/intents/orphaned.md")
	require.NoError(t, err)
	assert.True(t, exists, "deleting the cache must lose no content")
}

// A campaign with no commits has nothing uncommitted in the sense this check
// means. Without the history gate, the first doctor run in a new campaign
// reports camp's own scaffolding as lost queue content.
func TestIntegration_DoctorDoesNotFlagAFreshCampaignsScaffolding(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath := "/campaigns/jobs-doctor-fresh"
	_, err := tc.InitCampaign(campPath, "jobs-doctor-fresh", "product")
	require.NoError(t, err)

	stdout, stderr, _, err := tc.RunCampSplitInDir(campPath, "doctor", "-c", "jobs")
	require.NoError(t, err)

	combined := stdout + stderr
	assert.NotContains(t, combined, "uncommitted bookkeeping",
		"a never-committed campaign must not report its own scaffolding; output:\n%s", combined)
}
