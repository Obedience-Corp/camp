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

// A drain is only meaningful against the real binary: the property is that a
// user's next command sees the queued commit, and "the user's next command" is
// a separate process from the one that enqueued. These run the real camp in the
// container, with jobs written straight into the queue directory so a test can
// enqueue work no command yet produces (enqueuers land in sequence 03).

// writeJob puts a job file into a lane, the way an enqueuer will.
//
// Written as JSON rather than through jobs.Enqueue because these tests run
// against the binary in the container, which has no way to call into this
// process. The filename is the zero-padded sequence, which is also what makes
// the queue's collision check work.
func writeJob(t *testing.T, tc *TestContainer, campPath, laneSlug string, seq int, doc map[string]any) {
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

	dir := fmt.Sprintf("%s/.campaign/cache/jobs/pending/%s", campPath, laneSlug)
	tc.Shell(t, fmt.Sprintf("mkdir -p %s", dir))
	require.NoError(t, tc.WriteFile(fmt.Sprintf("%s/%07d.json", dir, seq), string(data)))
}

// rootLane is the campaign root's lane directory name. Percent-encoded so a
// submodule literally named "root" cannot collide with it.
const rootLane = "%2E"

// pendingJobCount counts job files left in a lane.
func pendingJobCount(t *testing.T, tc *TestContainer, campPath, state, laneSlug string) int {
	t.Helper()
	out := tc.Shell(t, fmt.Sprintf(
		"ls %s/.campaign/cache/jobs/%s/%s/*.json 2>/dev/null | wc -l",
		campPath, state, laneSlug))
	return atoiOrZero(strings.TrimSpace(out))
}

func atoiOrZero(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			continue
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// setupDrainCampaign builds a campaign with a bare remote wired up, so a push
// can be inspected as a real published ref rather than a local branch.
func setupDrainCampaign(t *testing.T, tc *TestContainer, name string) (campPath, remotePath string) {
	t.Helper()
	campPath = "/campaigns/" + name
	remotePath = "/remotes/" + name + ".git"

	_, err := tc.InitCampaign(campPath, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		mkdir -p /remotes
		git init -q --bare %[2]s
		cd %[1]s
		git remote add origin %[2]s
		git push -q -u origin HEAD 2>&1 | tail -1 || true
	`, campPath, remotePath))

	return campPath, remotePath
}

// remoteHasPath reports whether the remote's pushed branch contains a path.
func remoteHasPath(t *testing.T, tc *TestContainer, campPath, remotePath, path string) bool {
	t.Helper()
	branch := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "--abbrev-ref", "HEAD"))
	out := tc.Shell(t, fmt.Sprintf(
		"git -C %s ls-tree -r --name-only %s 2>/dev/null || true", remotePath, branch))
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == path {
			return true
		}
	}
	return false
}

// Deferred criterion 6 and the ordering barrier (criterion 1): a job enqueued
// before a push is in the pushed ref, not left behind on the machine.
//
// This is the failure the drain exists to prevent, and the one a user cannot
// see: without it the bookkeeping commit lands after the push and stays local
// forever, with nothing in any output saying so.
func TestIntegration_DrainPutsAQueuedCommitInThePush(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, remotePath := setupDrainCampaign(t, tc, "drain-push")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p .campaign/intents
		printf 'queued intent\n' > .campaign/intents/queued.md
	`, campPath))

	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind":    "commit-paths",
		"repo":    ".",
		"paths":   []string{".campaign/intents/queued.md"},
		"message": "capture intent: queued before the push",
	})

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "push")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	assert.True(t, remoteHasPath(t, tc, campPath, remotePath, ".campaign/intents/queued.md"),
		"the queued commit must be in the pushed ref, not left behind locally")
	assert.Zero(t, pendingJobCount(t, tc, campPath, "pending", rootLane),
		"the drain must leave the lane empty")
}

// Deferred criterion 37h: a drain against a non-empty lane that nobody is
// serving spawns a worker and completes, rather than waiting out its timeout.
//
// A drain that only waits turns a crashed worker, or a job enqueued by a
// process that died before spawning, into a wedged terminal.
func TestIntegration_DrainSpawnsAWorkerForAnUnservedLane(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "drain-spawn")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p .campaign/intents
		printf 'nobody is serving this\n' > .campaign/intents/unserved.md
	`, campPath))

	// No worker exists and no lane lock is present: nothing in the system is
	// going to run this job unless the drain starts something.
	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind":    "commit-paths",
		"repo":    ".",
		"paths":   []string{".campaign/intents/unserved.md"},
		"message": "capture intent: unserved lane",
	})

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "status")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	assert.Zero(t, pendingJobCount(t, tc, campPath, "pending", rootLane),
		"a drain must spawn a worker for an unserved lane rather than time out")

	log := tc.GitOutput(t, campPath, "log", "--oneline", "-5")
	assert.Contains(t, log, "unserved lane",
		"the spawned worker must have committed the job; git log:\n%s", log)
}

// Manifest jobs are exempt from every drain. Blocking a push on a first-pass
// hash of a large artifact root would put the latency this subsystem exists to
// remove straight back on the user's critical path.
//
// The job here names a path that does not exist, so if the drain ever waited
// for it the command would sit until its timeout instead of returning.
func TestIntegration_ManifestJobNeverBlocksAPush(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "drain-manifest")

	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind":    "commit-paths",
		"class":   "manifest",
		"repo":    ".",
		"paths":   []string{".campaign/manifests/videos.json"},
		"message": "record artifact manifest",
	})

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "push")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.NotContains(t, stderr, "waiting on",
		"a manifest job must not make a push wait; stderr:\n%s", stderr)
}

// A read-only command warns and proceeds when the queue outlasts it. Refusing
// to show status because a commit is slow is worse than showing status with a
// caveat, and status is the command people run to ask whether something
// committed.
//
// The lane is wedged deliberately: a fresh lock says a worker holds it, and the
// job names a path that does not exist so nothing can ever complete it.
func TestIntegration_StatusWarnsAndProceedsOnAWedgedLane(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "drain-status-wedged")

	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind":    "commit-paths",
		"repo":    ".",
		"paths":   []string{"does/not/exist.md"},
		"message": "capture intent: wedged",
	})
	// A fresh lane lock stands in for a live worker, so the drain does not
	// spawn one and simply waits.
	tc.Shell(t, fmt.Sprintf(`
		cd %s/.campaign/cache/jobs
		printf '99999\n' > worker-%s.lock
	`, campPath, rootLane))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath,
		"status", "--short")
	require.NoError(t, err)

	assert.Equal(t, 0, exitCode,
		"status must proceed despite a slow queue; stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stderr, "still queued",
		"status must say the report may be out of date; stderr:\n%s", stderr)
	assert.Contains(t, stderr, "camp jobs",
		"the warning must name the command that shows what is stuck; stderr:\n%s", stderr)
}

// A write command refuses rather than proceeding into a half-correct state,
// and every option in the refusal is a command the user can run as printed.
func TestIntegration_PushRefusesOnAWedgedLaneAndOffersAWayOut(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "drain-push-wedged")

	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind":    "commit-paths",
		"repo":    ".",
		"paths":   []string{"does/not/exist.md"},
		"message": "capture intent: wedged before push",
	})
	tc.Shell(t, fmt.Sprintf(`
		cd %s/.campaign/cache/jobs
		printf '99999\n' > worker-%s.lock
	`, campPath, rootLane))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "push")
	require.NoError(t, err)

	assert.NotEqual(t, 0, exitCode,
		"push must refuse rather than leave the queued commit unpushed; stdout:\n%s\nstderr:\n%s",
		stdout, stderr)
	combined := stdout + stderr
	assert.Contains(t, combined, "capture intent: wedged before push",
		"the refusal must name the blocking job; output:\n%s", combined)
	assert.Contains(t, combined, "--no-drain",
		"the refusal must carry its own way out; output:\n%s", combined)
	assert.Contains(t, combined, "camp jobs",
		"the refusal must say where to look; output:\n%s", combined)
}

// --no-drain is that way out, and it must actually work: a user told to rerun
// with a flag has to get past the wedge, not the same refusal again.
func TestIntegration_PushNoDrainSkipsTheWait(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "drain-push-nodrain")

	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind":    "commit-paths",
		"repo":    ".",
		"paths":   []string{"does/not/exist.md"},
		"message": "capture intent: skipped",
	})
	tc.Shell(t, fmt.Sprintf(`
		cd %s/.campaign/cache/jobs
		printf '99999\n' > worker-%s.lock
	`, campPath, rootLane))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "push", "--no-drain")
	require.NoError(t, err)

	assert.Equal(t, 0, exitCode,
		"--no-drain must get past the wedge; stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.NotContains(t, stderr, "not completed after",
		"--no-drain must not refuse; stderr:\n%s", stderr)
	assert.Equal(t, 1, pendingJobCount(t, tc, campPath, "pending", rootLane),
		"--no-drain skips the wait; it must not drop the job")
}

// Agent consideration 1: a machine caller needs to tell a slow queue from a
// stuck one, and total runtime cannot say which.
func TestIntegration_CommitJSONCarriesDrainWaitedMs(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "drain-json-field")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'content\n' > note.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath,
		"commit", "--json", "-m", "ordinary content")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	assert.Contains(t, stdout, `"drain_waited_ms"`,
		"the document must always carry the field, not only when it waited; got:\n%s", stdout)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc),
		"stdout must be exactly one JSON document; got:\n%s", stdout)
	waited, ok := doc["drain_waited_ms"].(float64)
	require.True(t, ok, "drain_waited_ms must be a number; got %T", doc["drain_waited_ms"])
	assert.Zero(t, waited, "an empty queue must report no wait")
}

// The same field on doctor, which also drains.
//
// Deliberately not `camp doctor --json | jq ...` inside tc.Shell. That form
// passed while the command was not running at all: the binary lives at /camp
// rather than on PATH, so the producer failed, jq read nothing, and the
// pipeline's exit code came from jq. The command's own exit code is asserted
// here, and jq reads a file rather than a pipe.
func TestIntegration_DoctorJSONCarriesDrainWaitedMs(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "drain-doctor-json")

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "doctor", "--json")
	require.NoError(t, err)
	// Doctor exits 1 when it finds warnings, which is not a failure of this test.
	require.LessOrEqual(t, exitCode, 1, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	require.NoError(t, tc.WriteFile("/tmp/doctor-drain.json", stdout))
	out := tc.Shell(t, "jq -r '.drain_waited_ms' /tmp/doctor-drain.json")
	assert.Equal(t, "0", strings.TrimSpace(out),
		"doctor --json must carry a numeric drain_waited_ms; document:\n%s", stdout)
}

// The commit drain is the barrier that keeps history ordered: anything queued
// before the user's commit is behind it in history, never interleaved with it.
func TestIntegration_CommitDrainsBeforeStaging(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "drain-commit-order")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p .campaign/intents
		printf 'queued first\n' > .campaign/intents/first.md
		printf 'user content\n' > user.md
	`, campPath))

	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind":    "commit-paths",
		"repo":    ".",
		"paths":   []string{".campaign/intents/first.md"},
		"message": "capture intent: queued first",
	})

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath,
		"commit", "-m", "user commit second")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	subjects := tc.GitOutput(t, campPath, "log", "--format=%s", "-3")
	lines := strings.Split(strings.TrimSpace(subjects), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "git log:\n%s", subjects)
	assert.Contains(t, lines[0], "user commit second",
		"the user's commit must be on top; git log:\n%s", subjects)
	assert.Contains(t, lines[1], "queued first",
		"the queued commit must be underneath it, not interleaved; git log:\n%s", subjects)
}
