//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A commit message writer that says it is temporarily unavailable, and what a
// real campaign does about it.
//
// None of this is observable from a package test. The subject the user is left
// with, the job moving back to pending without spending its attempts, and a
// drain that no longer has to wait for any of it are all properties of a real
// queue serving a real repository across several processes.

// unavailableWriter exits 75 with the diagnostic a daemon-backed writer prints
// when its daemon is not up. It is the 2026-09-14 outage, reproduced.
const unavailableWriter = `#!/bin/sh
echo "Usage:" >&2
echo "connect to daemon: ob: daemon not running" >&2
exit 75`

// recoveringWriter is unavailable until a sentinel file appears, then writes a
// real message. It stands in for a daemon that comes back.
const recoveringWriter = `#!/bin/sh
if [ ! -e /tmp/camp-writer-back ]; then
  echo "connect to daemon: ob: daemon not running" >&2
  exit 75
fi
echo "feat: written after the daemon came back"`

// configureUnavailableWriter points hooks.commit_message at a script and sets
// the retry window in the same block, which is the only way to write both
// without producing a second hooks: key.
func configureUnavailableWriter(t *testing.T, tc *TestContainer, campPath, name, script, retryWindow string) {
	t.Helper()
	tc.Shell(t, fmt.Sprintf(`
		mkdir -p /writers
		cat > /writers/%[1]s.sh <<'SCRIPT'
%[2]s
SCRIPT
		chmod +x /writers/%[1]s.sh
		cd %[3]s
		cat >> .campaign/campaign.yaml <<'YAML'
hooks:
  commit_message:
    command: /writers/%[1]s.sh
    retry_window: %[4]s
YAML
	`, name, script, campPath, retryWindow))
}

// queuedJob is the part of `camp jobs --json` these tests read.
type queuedJob struct {
	ID               string `json:"id"`
	State            string `json:"state"`
	Attempts         int    `json:"attempts"`
	WriterAttempts   int    `json:"writer_attempts"`
	WriterFallbackAt string `json:"writer_fallback_at"`
	WriterRetryAt    string `json:"writer_retry_at"`
	Summary          string `json:"summary"`
}

// readQueue returns what `camp jobs --json` reports. It never drains and never
// marks the lane as wanted, so watching the queue does not change what the
// worker decides to do.
func readQueue(t *testing.T, tc *TestContainer, campPath string) []queuedJob {
	t.Helper()
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "jobs", "--json")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	var doc struct {
		Jobs []queuedJob `json:"jobs"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "camp jobs --json:\n%s", stdout)
	return doc.Jobs
}

// awaitQueue polls the queue until want is satisfied, and fails with the last
// thing it saw when the deadline passes.
func awaitQueue(t *testing.T, tc *TestContainer, campPath, what string,
	timeout time.Duration, want func([]queuedJob) bool,
) []queuedJob {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last []queuedJob
	for time.Now().Before(deadline) {
		last = readQueue(t, tc, campPath)
		if want(last) {
			return last
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s; queue was %+v", timeout, what, last)
	return nil
}

// stageAndCommit stages a file and queues a deferred --auto-write commit.
func stageAndCommit(t *testing.T, tc *TestContainer, campPath, filename string) {
	t.Helper()
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'content\n' > %s
	`, campPath, filename))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
}

// headBody returns the body of HEAD's commit message.
func headBody(t *testing.T, tc *TestContainer, campPath string) string {
	t.Helper()
	return tc.GitOutput(t, campPath, "log", "-1", "--format=%b")
}

// The whole point of the exit-75 contract: a writer that is merely down buys
// the job a wait instead of a filler subject, the wait costs the job none of
// its crash-recovery attempts, and the commit still lands when the window is
// spent.
func TestIntegration_UnavailableWriterWaitsThenDescribesTheCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "aw-unavailable-wait")
	configureUnavailableWriter(t, tc, campPath, "unavailable", unavailableWriter, "8s")

	stageAndCommit(t, tc, campPath, "waited.md")

	waiting := awaitQueue(t, tc, campPath, "the job to go back to pending for the writer",
		30*time.Second, func(jobs []queuedJob) bool {
			return len(jobs) == 1 && jobs[0].WriterAttempts >= 1
		})

	job := waiting[0]
	assert.Equal(t, "pending", job.State,
		"an unavailable writer must return the job to pending, not park it")
	assert.Equal(t, 0, job.Attempts,
		"waiting for a writer must not spend the crash-recovery budget")
	assert.NotEmpty(t, job.WriterFallbackAt,
		"a waiting job must say when camp will stop waiting")
	assert.NotEmpty(t, job.WriterRetryAt,
		"a waiting job must say when it will ask again")

	// The window is spent and the commit lands rather than waiting forever.
	awaitQueue(t, tc, campPath, "the commit to land after the window expired",
		60*time.Second, func(jobs []queuedJob) bool { return len(jobs) == 0 })

	assert.Contains(t, headSubject(t, tc, campPath), "(writer unavailable)",
		"the commit must still land, marked, when the window is spent")

	body := headBody(t, tc, campPath)
	assert.Contains(t, body, "the writer was unreachable for",
		"the commit body must say camp waited; it is the only lasting record")
	assert.Contains(t, body, "attempts",
		"the body must say how many times camp asked")
	assert.Contains(t, body, "daemon not running",
		"the body must keep the writer's own diagnostic")

	// Two distinct log lines, because they are two distinct events: one job
	// still coming, one subject the user is stuck with.
	log := tc.Shell(t, fmt.Sprintf("cat %s/.campaign/cache/jobs/worker.log", campPath))
	assert.Contains(t, log, "writer-unavailable", "the wait must be recorded")
	assert.Contains(t, log, "writer-degraded", "the fallback must be recorded")
}

// retry_window: 0 is how a user asks for the behavior camp had before the
// contract existed, and it has to mean exactly that.
func TestIntegration_UnavailableWriterWithNoWindowLandsImmediately(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "aw-unavailable-nowindow")
	configureUnavailableWriter(t, tc, campPath, "unavailable", unavailableWriter, "\"0\"")

	stageAndCommit(t, tc, campPath, "immediate.md")
	drainJobs(t, tc, campPath)

	assert.Contains(t, headSubject(t, tc, campPath), "(writer unavailable)")
	assert.NotContains(t, headBody(t, tc, campPath), "camp waited for it",
		"a commit that never waited must not claim it did")
}

// The wait is worth having only if a writer that comes back is used. A retry
// whose writer answers produces the writer's own message, with no marker and
// no trace of the outage in the subject.
func TestIntegration_WriterThatComesBackInsideTheWindowWritesTheMessage(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "aw-unavailable-recovers")
	tc.Shell(t, "rm -f /tmp/camp-writer-back")
	configureUnavailableWriter(t, tc, campPath, "recovering", recoveringWriter, "6s")

	stageAndCommit(t, tc, campPath, "recovered.md")

	awaitQueue(t, tc, campPath, "the first unavailable attempt",
		30*time.Second, func(jobs []queuedJob) bool {
			return len(jobs) == 1 && jobs[0].WriterAttempts >= 1
		})

	// The daemon comes back while the job is waiting.
	tc.Shell(t, "touch /tmp/camp-writer-back")
	t.Cleanup(func() { tc.Shell(t, "rm -f /tmp/camp-writer-back") })

	awaitQueue(t, tc, campPath, "the retry to land the writer's message",
		60*time.Second, func(jobs []queuedJob) bool { return len(jobs) == 0 })

	subject := headSubject(t, tc, campPath)
	assert.Contains(t, subject, "feat: written after the daemon came back",
		"a writer that came back must get to name the commit")
	assert.NotContains(t, subject, "(writer unavailable)",
		"a commit the writer described must not be marked as degraded")
}

// The wait is free only while nobody is waiting on it. A command that blocks on
// the lane ends it immediately, which is what keeps a daemon outage from
// turning every later camp command into a thirty-second refusal.
func TestIntegration_ADrainEndsTheWaitForAnUnavailableWriter(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "aw-unavailable-drain")
	// Long enough that a drain which had to sit out the wait could not pass.
	configureUnavailableWriter(t, tc, campPath, "unavailable", unavailableWriter, "10m")

	stageAndCommit(t, tc, campPath, "blocked.md")

	awaitQueue(t, tc, campPath, "the job to start waiting for the writer",
		30*time.Second, func(jobs []queuedJob) bool {
			return len(jobs) == 1 && jobs[0].WriterAttempts >= 1
		})

	// A waiting row explains itself rather than reading as an idle "pending".
	stdout, _, _, err := tc.RunCampSplitInDir(campPath, "jobs", "--plain")
	require.NoError(t, err)
	assert.Contains(t, stdout, "waiting for the message writer",
		"a waiting job must say what it is waiting for; output:\n%s", stdout)
	assert.Contains(t, stdout, "falls back at",
		"a waiting job must say when the wait ends; output:\n%s", stdout)

	start := time.Now()
	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "jobs", "drain")
	waited := time.Since(start)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode,
		"a drain must not have to sit out the writer's retry window; stderr:\n%s", stderr)
	assert.Less(t, waited, 25*time.Second,
		"the drain returned in %s; a blocked command must end the wait, not join it", waited)

	assert.Contains(t, headSubject(t, tc, campPath), "(writer unavailable)",
		"ending the wait means landing the commit camp can describe itself")
	assert.Contains(t, strings.ToLower(headBody(t, tc, campPath)), "unreachable for",
		"a commit that waited before it landed must say so")
}
