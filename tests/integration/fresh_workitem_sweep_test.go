//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addFreshEligibleWorkitem creates a design workitem with a completed workflow
// run at the campaign root and commits it, making it eligible for the tier-1
// sweep that camp fresh runs.
func addFreshEligibleWorkitem(t *testing.T, tc *TestContainer, campaignPath, slug string) {
	t.Helper()
	out, err := tc.RunCampInDir(campaignPath,
		"workitem", "create", slug, "--type", "design", "--title", slug, "--id", "design-"+slug)
	require.NoError(t, err, "workitem create: %s", out)
	base := campaignPath + "/workflow/design/" + slug + "/.workflow"
	require.NoError(t, tc.WriteFile(base+"/workflow.yaml", "workflow_id: wf-"+slug+"\nactive_run_id: r1\n"))
	require.NoError(t, tc.WriteFile(base+"/runs/r1/run.yaml", "status: active\nsummary:\n  total_steps: 1\n"))
	require.NoError(t, tc.WriteFile(base+"/runs/r1/progress_events.jsonl",
		`{"event_type":"workflow_run_started"}
{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"workflow_run_completed"}
`))
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

// Scenario 3 (default "sweep"): single-project fresh promotes the eligible item
// exactly once.
func TestIntegration_FreshSweep_DefaultPromotes(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "freshsweep-default")
	addFreshEligibleWorkitem(t, tc, campaignPath, "done-feature")

	out, err := tc.RunCampInDir(campaignPath, "fresh", "test-project", "--no-push")
	require.NoError(t, err, "fresh: %s", out)

	assert.Equal(t, 1, countSweepEvidence(t, tc, campaignPath), "sweep ran once")
	gone, err := tc.CheckDirExists(campaignPath + "/workflow/design/done-feature")
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
	stays, err := tc.CheckDirExists(campaignPath + "/workflow/design/done-feature")
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
	stays, err := tc.CheckDirExists(campaignPath + "/workflow/design/done-feature")
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

	out, err := tc.RunCampInDir(campaignPath, "fresh", "all", "--no-push")
	require.NoError(t, err, "fresh all: %s", out)

	assert.Equal(t, 1, countSweepEvidence(t, tc, campaignPath),
		"sweep must run exactly once for the whole batch, not once per project")
	gone, err := tc.CheckDirExists(campaignPath + "/workflow/design/done-feature")
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

	// Make project A dirty so its fresh cycle fails freshSafetyChecks.
	require.NoError(t, tc.WriteFile(projectDirA+"/dirty.txt", "uncommitted"))

	out, err := tc.RunCampInDir(campaignPath, "fresh", "all", "--no-push")
	require.Error(t, err, "batch should report failure for the dirty project: %s", out)

	assert.Equal(t, 1, countSweepEvidence(t, tc, campaignPath),
		"sweep still runs once despite the per-project failure")
	gone, err := tc.CheckDirExists(campaignPath + "/workflow/design/done-feature")
	require.NoError(t, err)
	assert.False(t, gone, "eligible item should have moved despite the failed project")
}
