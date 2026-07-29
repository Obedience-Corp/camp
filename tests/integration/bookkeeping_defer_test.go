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

// Camp's own bookkeeping is what the queue exists for. A user running
// `camp intent add` asked to capture an idea, not to wait for a commit, and
// these tests are about what happens to the repository between those two
// moments.

// Deferred criterion 1: enqueue then drain produces exactly one commit
// containing exactly the job's paths.
func TestIntegration_BookkeepingCommitLandsAfterTheCommandReturns(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "bk-defer-lands")

	before := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "intent", "add", "a deferred idea")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	// The file exists immediately: only the commit is deferred, never the write.
	// A user who was told their idea was captured must find it on disk.
	listing := tc.Shell(t, fmt.Sprintf("ls %s/.campaign/intents/inbox/", campPath))
	assert.Contains(t, listing, "a-deferred-idea",
		"the intent file must be written in the foreground; inbox:\n%s", listing)

	// The command reported a queued commit rather than a made one. Asserting
	// HEAD has not moved yet would race a worker that is often faster than the
	// next line of the test; "returned before the commit" is the property, not
	// "the commit is still outstanding at some later instant".
	assert.Contains(t, stdout+stderr, "queued",
		"the command must report a queued commit, not a made one; output:\n%s", stdout+stderr)

	drainJobs(t, tc, campPath)

	after := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))
	assert.NotEqual(t, before, after, "the queued commit must land once the worker runs")

	subject := strings.TrimSpace(tc.GitOutput(t, campPath, "log", "-1", "--format=%s"))
	assert.Contains(t, subject, "a deferred idea",
		"the deferred commit must carry the deterministic bookkeeping message; got %q", subject)

	// Exactly one commit, not two: a deferred job commits once.
	count := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-list", "--count", before+"..HEAD"))
	assert.Equal(t, "1", count, "the drain must produce exactly one commit, got %s", count)
}

// Deferred criterion 2: a bookkeeping job leaves a concurrently staged index
// byte-identical.
//
// This is the rule the whole design rests on. Camp's background work runs
// against a working tree the user is still using, so a job that touched the
// real index would sweep their in-progress staging into camp's commit.
func TestIntegration_BookkeepingJobLeavesTheUsersIndexAlone(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "bk-defer-index")

	// The user is midway through staging their own work.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'mine\n' > user-work.md
		printf 'also mine\n' > user-work-2.md
		git add user-work.md user-work-2.md
	`, campPath))

	stagedBefore := tc.GitOutput(t, campPath, "status", "--short")
	blobsBefore := tc.GitOutput(t, campPath, "ls-files", "-s", "user-work.md", "user-work-2.md")

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "intent", "add", "captured mid-staging")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	drainJobs(t, tc, campPath)

	stagedAfter := tc.GitOutput(t, campPath, "status", "--short")
	blobsAfter := tc.GitOutput(t, campPath, "ls-files", "-s", "user-work.md", "user-work-2.md")

	// Two different claims, both load-bearing. The status must show the user's
	// two files and nothing else, which catches camp leaving its own paths
	// staged or, worse, showing a file it just committed as staged-for-deletion
	// (what skipping the post-commit index update would produce). The blobs
	// must be identical, which catches camp rewriting their content.
	assert.Equal(t, stagedBefore, stagedAfter,
		"camp's background commit changed what the user had staged")
	assert.Equal(t, blobsBefore, blobsAfter,
		"the user's staged blobs must be untouched by a background commit")

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.NotContains(t, committed, "user-work.md",
		"camp's commit swept in the user's staged work; it contained:\n%s", committed)
}

// Deferred criterion 7: paths deleted since the enqueue are a success without a
// commit, not a failure.
//
// The content may already have been committed by a drain, or the user may have
// removed it. Failing would park a job that has nothing left to do, and the
// failed-job notice would then nag about work nobody wants.
func TestIntegration_BookkeepingJobWhosePathsVanishedSucceeds(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "bk-defer-vanished")

	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind": "commit-paths", "repo": ".",
		"paths":   []string{"never-existed.md"},
		"message": "capture intent: gone before the worker ran",
	})

	drainJobs(t, tc, campPath)

	assert.Zero(t, pendingJobCount(t, tc, campPath, "failed", rootLane),
		"a job with nothing left to commit must complete, not fail")

	_, stderr, _, err := tc.RunCampSplitInDir(campPath, "status", "--short")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "deferred commit failed",
		"nothing to do is not a failure; stderr:\n%s", stderr)
}

// Deferred criterion 10: CAMP_NO_DEFER=1 restores the pre-deferral behavior
// exactly, so a harness that needs determinism has one switch.
func TestIntegration_CampNoDeferMakesBookkeepingSynchronous(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "bk-defer-nodefer")

	before := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))

	out := tc.Shell(t, fmt.Sprintf(
		"cd %s && CAMP_NO_DEFER=1 /camp intent add 'an inline idea' 2>&1", campPath))

	after := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))
	assert.NotEqual(t, before, after,
		"CAMP_NO_DEFER must commit before the command returns; output:\n%s", out)
	assert.NotContains(t, out, "queued",
		"CAMP_NO_DEFER must not queue anything; output:\n%s", out)

	stdout, _, _, err := tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	assert.Contains(t, stdout, "No deferred commits queued",
		"CAMP_NO_DEFER must create no job; camp jobs:\n%s", stdout)
}

// A repository with commit hooks keeps bookkeeping synchronous too. The hook is
// the user's code and it expects to run.
func TestIntegration_BookkeepingStaysSynchronousWithCommitHooks(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "bk-defer-hooks")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		cat > .git/hooks/pre-commit <<'HOOK'
#!/bin/sh
echo "bookkeeping hook ran" > /tmp/bk-hook-sentinel
HOOK
		chmod +x .git/hooks/pre-commit
		rm -f /tmp/bk-hook-sentinel
	`, campPath))

	before := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "intent", "add", "hooked idea")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	after := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))
	assert.NotEqual(t, before, after,
		"a hook repository must commit synchronously; output:\n%s", stdout+stderr)

	ran, err := tc.CheckFileExists("/tmp/bk-hook-sentinel")
	require.NoError(t, err)
	assert.True(t, ran, "the user's pre-commit hook must have run")
}

// Dropping a bookkeeping job leaves its content for the next ordinary commit.
// The pair is what makes drop safe: camp stops trying, the user keeps the work.
func TestIntegration_DroppedBookkeepingJobLeavesTheIntentOnDisk(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "bk-defer-drop")

	// A job that can never succeed, so it reaches failed/ and can be dropped.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p .campaign/intents/inbox
		printf 'the idea survives\n' > .campaign/intents/inbox/survivor.md
	`, campPath))
	writeFailedJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind": "commit-paths", "repo": ".",
		"paths":    []string{".campaign/intents/inbox/survivor.md"},
		"message":  "capture intent: survivor",
		"attempts": 3,
	})

	_, _, exitCode, err := tc.RunCampSplitInDir(campPath, "jobs", "drop", "all")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	exists, err := tc.CheckFileExists(campPath + "/.campaign/intents/inbox/survivor.md")
	require.NoError(t, err)
	require.True(t, exists, "drop must never delete the content")

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "-m", "pick up the dropped idea")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	tracked := tc.GitOutput(t, campPath, "ls-files", ".campaign/intents/inbox/survivor.md")
	assert.Contains(t, tracked, "survivor.md",
		"an ordinary commit must pick up what the dropped job would have committed")
}
