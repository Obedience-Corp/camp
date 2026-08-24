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

type completionCommandResult struct {
	SchemaVersion string `json:"schema_version"`
	Workitem      struct {
		ID   string `json:"id"`
		Ref  string `json:"ref"`
		Path string `json:"path"`
	} `json:"workitem"`
	Before struct {
		Policy        string `json:"policy"`
		ReviewedRunID string `json:"reviewed_run_id"`
	} `json:"before"`
	After struct {
		Policy        string `json:"policy"`
		ReviewedRunID string `json:"reviewed_run_id"`
	} `json:"after"`
	Changed   bool `json:"changed"`
	Adopted   bool `json:"adopted"`
	Committed bool `json:"committed"`
}

func runCompletionJSON(t *testing.T, tc *TestContainer, path, selector, decision string) completionCommandResult {
	t.Helper()
	out, err := tc.RunCampInDir(path, "workitem", "completion", selector, decision, "--json")
	require.NoError(t, err, "completion command: %s", out)
	var result completionCommandResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &result), "completion JSON: %s", out)
	assert.Equal(t, "workitem-completion/v1alpha1", result.SchemaVersion)
	return result
}

func stampSecondCompletedRun(t *testing.T, tc *TestContainer, campaignPath, wkType, slug string) {
	t.Helper()
	base := campaignPath + "/workflow/" + wkType + "/" + slug + "/.workflow"
	require.NoError(t, tc.WriteFile(base+"/workflow.yaml",
		"workflow_id: wf-"+slug+"\nruns:\n    - run_id: r1\n      status: completed\n      ended_at: \"2026-07-24T19:00:00Z\"\n    - run_id: r2\n      status: completed\n      ended_at: \"2026-07-25T19:00:00Z\"\n"))
	require.NoError(t, tc.WriteFile(base+"/runs/r2/run.yaml", "status: completed\nsummary:\n  total_steps: 1\n"))
	require.NoError(t, tc.WriteFile(base+"/runs/r2/progress_events.jsonl",
		"{\"event_type\":\"workflow_run_started\"}\n{\"event_type\":\"wf_step_start\"}\n{\"event_type\":\"wf_step_done\"}\n{\"event_type\":\"workflow_run_completed\"}\n"))
}

func TestIntegration_WorkitemCompletion_AcknowledgeOneRunThenReviewNext(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "completion-acknowledge")
	createSweepWorkitem(t, tc, path, "explore", "provider-scan")
	stampCompletedRun(t, tc, path, "explore", "provider-scan")
	backdateWorkitemContent(t, tc, path, "explore", "provider-scan")
	commitFixture(t, tc, path)
	require.NoError(t, tc.WriteFile(path+"/unrelated.txt", "must remain uncommitted\n"))

	result := runCompletionJSON(t, tc, path, "explore-provider-scan-fixed", "acknowledge")
	assert.True(t, result.Changed)
	assert.True(t, result.Committed)
	assert.Equal(t, "r1", result.After.ReviewedRunID)

	marker, err := tc.ReadFile(path + "/workflow/explore/provider-scan/.workitem")
	require.NoError(t, err)
	assert.Contains(t, marker, "version: v1alpha9")
	assert.Contains(t, marker, "completion_reviewed_run_id: r1")
	assert.NotContains(t, marker, "completion_policy:")
	assert.Equal(t, 0, runSweepJSON(t, tc, path, "--dry-run").Candidates,
		"the acknowledged latest run must be silent")

	committed := tc.GitOutput(t, path, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "workflow/explore/provider-scan/.workitem")
	assert.Contains(t, committed, ".campaign/workitems/.workitems.jsonl")
	assert.NotContains(t, committed, "unrelated.txt")
	status := tc.GitOutput(t, path, "status", "--porcelain", "--", "unrelated.txt")
	assert.Contains(t, status, "?? unrelated.txt", "unrelated dirty state must remain outside the decision commit")

	stampSecondCompletedRun(t, tc, path, "explore", "provider-scan")
	backdateWorkitemContent(t, tc, path, "explore", "provider-scan")
	res := runSweepJSON(t, tc, path, "--dry-run")
	assert.Equal(t, 1, res.Candidates, "a newer completed run must reopen review")
	require.Len(t, res.Items, 1)
	assert.Equal(t, "r2", res.Items[0].RunID)
}

func TestIntegration_WorkitemCompletion_RecurringIsDurableAndReversible(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "completion-recurring")
	createSweepWorkitem(t, tc, path, "explore", "weekly-scan")
	stampCompletedRun(t, tc, path, "explore", "weekly-scan")
	backdateWorkitemContent(t, tc, path, "explore", "weekly-scan")
	commitFixture(t, tc, path)

	result := runCompletionJSON(t, tc, path, "explore-weekly-scan-fixed", "recurring")
	assert.Equal(t, "recurring", result.After.Policy)
	assert.True(t, result.Committed)
	assert.Equal(t, 0, runSweepJSON(t, tc, path, "--dry-run").Candidates)

	stampSecondCompletedRun(t, tc, path, "explore", "weekly-scan")
	assert.Equal(t, 0, runSweepJSON(t, tc, path, "--dry-run").Candidates,
		"recurring suppresses future completed runs too")

	restored := runCompletionJSON(t, tc, path, "explore-weekly-scan-fixed", "review")
	assert.Equal(t, "review", restored.After.Policy)
	marker, err := tc.ReadFile(path + "/workflow/explore/weekly-scan/.workitem")
	require.NoError(t, err)
	assert.NotContains(t, marker, "completion_policy:")
	backdateWorkitemContent(t, tc, path, "explore", "weekly-scan")
	assert.Equal(t, 1, runSweepJSON(t, tc, path, "--dry-run").Candidates,
		"restoring review makes the existing latest completion actionable")
}

func TestIntegration_WorkitemCompletion_AdoptsLegacyDirectory(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "completion-adopt")
	require.NoError(t, tc.WriteFile(path+"/workflow/explore/legacy-scan/README.md", "# Legacy scan\n"))
	stampCompletedRun(t, tc, path, "explore", "legacy-scan")
	backdateWorkitemContent(t, tc, path, "explore", "legacy-scan")
	commitFixture(t, tc, path)

	result := runCompletionJSON(t, tc, path, "explore:workflow/explore/legacy-scan", "recurring")
	assert.True(t, result.Adopted)
	assert.NotEmpty(t, result.Workitem.ID)
	assert.NotEmpty(t, result.Workitem.Ref)
	marker, err := tc.ReadFile(path + "/workflow/explore/legacy-scan/.workitem")
	require.NoError(t, err)
	assert.Contains(t, marker, "completion_policy: recurring")
	assert.Contains(t, marker, "ref: WI-")
}
