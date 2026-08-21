//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A campaign-root message writer is unrelated to the project branch fresh is
// cycling. Fresh must complete the project work immediately, then capture its
// deterministic sweep commit behind the existing root job without touching
// the user's real index.
func TestIntegration_FreshQueuesSweepBehindBusyRootLane(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "freshsweep-root-queue")
	addFreshEligibleWorkitem(t, tc, campaignPath, "done-feature")
	enableAutoSweep(t, tc, campaignPath)
	tc.EnableDeferral()

	writeJob(t, tc, campaignPath, rootLane, 1, map[string]any{
		"kind": "commit-paths", "repo": ".",
		"paths":   []string{"held-message.md"},
		"message": "root commit already writing its message",
	})
	tc.Shell(t, fmt.Sprintf(`
		cd %s/.campaign/cache/jobs
		printf '99999\n' > worker-%s.lock
	`, campaignPath, rootLane))
	headBefore := strings.TrimSpace(tc.GitOutput(t, campaignPath, "rev-parse", "HEAD"))

	started := time.Now()
	out, err := tc.RunCampInDir(campaignPath, "fresh", "test-project",
		"--no-push", "--no-follow-up", "--no-prune")
	elapsed := time.Since(started)
	require.NoError(t, err, "fresh: %s", out)
	assert.Less(t, elapsed, 15*time.Second,
		"fresh waited on an unrelated root job for %s; output:\n%s", elapsed, out)
	assert.Equal(t, headBefore, strings.TrimSpace(tc.GitOutput(t, campaignPath, "rev-parse", "HEAD")),
		"the sweep must be queued behind the held root commit, not committed ahead of it")
	assert.Equal(t, 2, pendingJobCount(t, tc, campaignPath, "pending", rootLane),
		"the deterministic sweep commit must join the root lane behind the existing job")
	assert.Empty(t, strings.TrimSpace(tc.GitOutput(t, campaignPath, "diff", "--cached", "--name-only")),
		"queueing the sweep must not stage source deletions in the user's real index")

	queued := tc.Shell(t, fmt.Sprintf(
		"grep -l 'workflow/chore/done-feature' %s/.campaign/cache/jobs/pending/%s/*.json",
		campaignPath, rootLane))
	assert.NotEmpty(t, strings.TrimSpace(queued),
		"the queued sweep must capture the moved workitem's source deletion")

	tc.Shell(t, fmt.Sprintf("rm -f %s/.campaign/cache/jobs/worker-%s.lock", campaignPath, rootLane))
	drainJobs(t, tc, campaignPath)
	assert.NotEqual(t, headBefore, strings.TrimSpace(tc.GitOutput(t, campaignPath, "rev-parse", "HEAD")),
		"the queued sweep commit must land after the root lane is released")
	assert.Empty(t, strings.TrimSpace(tc.GitOutput(t, campaignPath, "status", "--porcelain", "--",
		"workflow/chore")),
		"the landed sweep commit must contain both sides of the workitem move")
}

// addFreshEligibleWorkitem creates a chore workitem with a completed workflow run
// at the campaign root and commits it, making it eligible for the tier-1 sweep
// that camp fresh runs. A chore rather than a design because run completion is
// only sufficient evidence for work whose loop WAS the work; a design would be
// reported as awaiting implementation and never moved.
//
// Its content is backdated past the fresh-write window: a fixture written moments
// before the sweep is exactly the "a session is still writing here" shape the
// guard exists to catch.
func addFreshEligibleWorkitem(t *testing.T, tc *TestContainer, campaignPath, slug string) {
	t.Helper()
	out, err := tc.RunCampInDir(campaignPath,
		"workitem", "create", slug, "--type", "chore", "--title", slug, "--id", "chore-"+slug)
	require.NoError(t, err, "workitem create: %s", out)
	stampCompletedRun(t, tc, campaignPath, "chore", slug)
	backdateWorkitemContent(t, tc, campaignPath, "chore", slug)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git add -A && git commit -q -m 'add eligible workitem'")
	require.NoError(t, err)
}

// countSweepEvidence counts audit entries carrying the sweep evidence marker.
func countSweepEvidence(t *testing.T, tc *TestContainer, campaignPath string) int {
	t.Helper()
	exists, err := tc.CheckFileExists(campaignPath + "/.campaign/workitems/.workitems.jsonl")
	require.NoError(t, err)
	if !exists {
		return 0
	}
	body, err := tc.ReadFile(campaignPath + "/.campaign/workitems/.workitems.jsonl")
	require.NoError(t, err)
	n := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, `"evidence":"workflow_run_completed"`) {
			n++
		}
	}
	return n
}

func setCompletedRuns(t *testing.T, tc *TestContainer, campaignPath, mode string) {
	t.Helper()
	require.NoError(t, tc.WriteFile(campaignPath+"/.campaign/settings/fresh.yaml",
		"completed_runs: \""+mode+"\"\n"))
}

// enableAutoSweep opts the campaign into the pre-2026-08 automatic behavior. The
// default is now "prompt", which reports on the non-TTY these tests run on, so a
// test about the MOVE has to ask for the mode that moves.
func enableAutoSweep(t *testing.T, tc *TestContainer, campaignPath string) {
	t.Helper()
	setCompletedRuns(t, tc, campaignPath, "sweep")
	commitFixture(t, tc, campaignPath)
}

// Scenario 3 (default "prompt", non-TTY): fresh reports and moves nothing. This
// is the behavior change: a fresh run with no configuration used to promote every
// workitem with a completed run, and an agent's fresh run is always a non-TTY, so
// it now reports instead of moving directories on its own.
func TestIntegration_FreshSweep_DefaultPromptReportsOnNonTTY(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "freshsweep-default")
	addFreshEligibleWorkitem(t, tc, campaignPath, "done-feature")

	out, err := tc.RunCampInDir(campaignPath, "fresh", "test-project", "--no-push")
	require.NoError(t, err, "fresh: %s", out)

	assert.Contains(t, out, "completed runs; run camp workitem sweep --prompt",
		"the default must report what it found")
	assert.Equal(t, 0, countSweepEvidence(t, tc, campaignPath), "the default must not promote")
	stays, err := tc.CheckDirExists(campaignPath + "/workflow/chore/done-feature")
	require.NoError(t, err)
	assert.True(t, stays, "the default must not move the item")
}

// Scenario 3b ("sweep" opt-in): the pre-2026-08 behavior is still available and
// still promotes the eligible item exactly once.
func TestIntegration_FreshSweep_SweepOptInPromotes(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "freshsweep-optin")
	addFreshEligibleWorkitem(t, tc, campaignPath, "done-feature")
	enableAutoSweep(t, tc, campaignPath)

	out, err := tc.RunCampInDir(campaignPath, "fresh", "test-project", "--no-push")
	require.NoError(t, err, "fresh: %s", out)

	assert.Equal(t, 1, countSweepEvidence(t, tc, campaignPath), "sweep ran once")
	gone, err := tc.CheckDirExists(campaignPath + "/workflow/chore/done-feature")
	require.NoError(t, err)
	assert.False(t, gone, "eligible item should have moved to the dungeon")
}

// Scenario 1 ("off"): no discovery, no move, no audit entry, even with an
// eligible item.
func TestIntegration_FreshSweep_OffIsNoop(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "freshsweep-off")
	addFreshEligibleWorkitem(t, tc, campaignPath, "done-feature")
	setCompletedRuns(t, tc, campaignPath, "off")
	_, _, err := tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git add -A && git commit -q -m 'set completed_runs off'")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(campaignPath, "fresh", "test-project", "--no-push")
	require.NoError(t, err, "fresh: %s", out)

	assert.Equal(t, 0, countSweepEvidence(t, tc, campaignPath), "off must not sweep")
	stays, err := tc.CheckDirExists(campaignPath + "/workflow/chore/done-feature")
	require.NoError(t, err)
	assert.True(t, stays, "off must not move the item")
}

// Scenario 2 ("report"): the eligible item is NOT moved but the banner prints.
func TestIntegration_FreshSweep_ReportPrintsBanner(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "freshsweep-report")
	addFreshEligibleWorkitem(t, tc, campaignPath, "done-feature")
	setCompletedRuns(t, tc, campaignPath, "report")
	_, _, err := tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git add -A && git commit -q -m 'set completed_runs report'")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(campaignPath, "fresh", "test-project", "--no-push")
	require.NoError(t, err, "fresh: %s", out)

	assert.Contains(t, out, "completed runs; run camp workitem sweep", "report prints the banner")
	assert.Equal(t, 0, countSweepEvidence(t, tc, campaignPath), "report must not sweep")
	stays, err := tc.CheckDirExists(campaignPath + "/workflow/chore/done-feature")
	require.NoError(t, err)
	assert.True(t, stays, "report must not move the item")
}

// Scenario 4 (once-per-invocation regression): a batch across two submodules
// with ONE eligible workitem sweeps exactly once, not once per project. Two
// projects already distinguish "once" (1 entry) from "per-project" (2 entries).
func TestIntegration_FreshSweep_AllRunsExactlyOnce(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, _, _ := setupFreshCampaignWithTwoSubmodules(t, tc, "freshsweep-all")
	addFreshEligibleWorkitem(t, tc, campaignPath, "done-feature")
	enableAutoSweep(t, tc, campaignPath)

	out, err := tc.RunCampInDir(campaignPath, "fresh", "all", "--no-push")
	require.NoError(t, err, "fresh all: %s", out)

	assert.Equal(t, 1, countSweepEvidence(t, tc, campaignPath),
		"sweep must run exactly once for the whole batch, not once per project")
	gone, err := tc.CheckDirExists(campaignPath + "/workflow/chore/done-feature")
	require.NoError(t, err)
	assert.False(t, gone, "eligible item should have moved once")
}

// Scenario 5: the campaign-root sweep runs once at the end of the batch even
// when a per-project fresh cycle fails (project made dirty so freshSafetyChecks
// rejects it). Decision: the sweep runs regardless of per-project failures,
// because completed-workitem promotion is independent of any project's
// git-hygiene outcome.
func TestIntegration_FreshSweep_RunsDespitePerProjectFailure(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, projectDirA, _ := setupFreshCampaignWithTwoSubmodules(t, tc, "freshsweep-partial")
	addFreshEligibleWorkitem(t, tc, campaignPath, "done-feature")
	enableAutoSweep(t, tc, campaignPath)

	// Make project A dirty so its fresh cycle fails freshSafetyChecks.
	require.NoError(t, tc.WriteFile(projectDirA+"/dirty.txt", "uncommitted"))

	out, err := tc.RunCampInDir(campaignPath, "fresh", "all", "--no-push")
	require.Error(t, err, "batch should report failure for the dirty project: %s", out)

	assert.Equal(t, 1, countSweepEvidence(t, tc, campaignPath),
		"sweep still runs once despite the per-project failure")
	gone, err := tc.CheckDirExists(campaignPath + "/workflow/chore/done-feature")
	require.NoError(t, err)
	assert.False(t, gone, "eligible item should have moved despite the failed project")
}
