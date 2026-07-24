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

// sweepResultItem mirrors one entry of the workitemSweepResult --json envelope
// (internal/commands/workitem/sweep.go). Kept local so a schema change here is a
// deliberate, reviewable edit.
type sweepResultItem struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	From        string `json:"from"`
	To          string `json:"to"`
	Evidence    string `json:"evidence"`
	ActiveRunID string `json:"active_run_id"`
	Committed   bool   `json:"committed"`
	Error       string `json:"error"`
}

type sweepResult struct {
	SchemaVersion string            `json:"schema_version"`
	DryRun        bool              `json:"dry_run"`
	Candidates    int               `json:"candidates"`
	Swept         int               `json:"swept"`
	Failed        int               `json:"failed"`
	Committed     bool              `json:"committed"`
	Items         []sweepResultItem `json:"items"`
}

// stampCompletedRun writes a fest-style .workflow/ runtime whose active run has
// completed, making the workitem at <campaign>/workflow/<wkType>/<slug>
// eligible for tier-1 sweep. Only the active run's events are replayed by camp's
// localrun loader, so a single completed run is enough.
func stampCompletedRun(t *testing.T, tc *TestContainer, campaignPath, wkType, slug string) {
	t.Helper()
	base := campaignPath + "/workflow/" + wkType + "/" + slug + "/.workflow"
	require.NoError(t, tc.WriteFile(base+"/workflow.yaml", "workflow_id: wf-"+slug+"\nactive_run_id: r1\n"))
	require.NoError(t, tc.WriteFile(base+"/runs/r1/run.yaml", "status: active\nsummary:\n  total_steps: 1\n"))
	require.NoError(t, tc.WriteFile(base+"/runs/r1/progress_events.jsonl",
		`{"event_type":"workflow_run_started"}
{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"workflow_run_completed"}
`))
}

// stampActiveRun writes an in-progress run (no completed event), so the item is
// discovered but not sweep-eligible.
func stampActiveRun(t *testing.T, tc *TestContainer, campaignPath, wkType, slug string) {
	t.Helper()
	base := campaignPath + "/workflow/" + wkType + "/" + slug + "/.workflow"
	require.NoError(t, tc.WriteFile(base+"/workflow.yaml", "workflow_id: wf-"+slug+"\nactive_run_id: r1\n"))
	require.NoError(t, tc.WriteFile(base+"/runs/r1/run.yaml", "status: active\nsummary:\n  total_steps: 2\n"))
	require.NoError(t, tc.WriteFile(base+"/runs/r1/progress_events.jsonl",
		`{"event_type":"workflow_run_started"}
{"event_type":"wf_step_start"}
`))
}

func createSweepWorkitem(t *testing.T, tc *TestContainer, campaignPath, wkType, slug string) {
	t.Helper()
	out, err := tc.RunCampInDir(campaignPath,
		"workitem", "create", slug, "--type", wkType, "--title", slug, "--id", wkType+"-"+slug+"-fixed")
	require.NoError(t, err, "workitem create should succeed: %s", out)
}

func commitFixture(t *testing.T, tc *TestContainer, path string) {
	t.Helper()
	_, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git add -A && git commit -q -m 'fixture'")
	require.NoError(t, err)
}

func runSweepJSON(t *testing.T, tc *TestContainer, path string, extraArgs ...string) sweepResult {
	t.Helper()
	args := append([]string{"workitem", "sweep", "--json"}, extraArgs...)
	out, err := tc.RunCampInDir(path, args...)
	require.NoError(t, err, "json sweep returns nil even with per-item failures: %s", out)
	var res sweepResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &res), "must parse: %s", out)
	assert.Equal(t, "workitem-sweep/v1alpha1", res.SchemaVersion)
	return res
}

// Scenario 1: zero eligible items -> empty result, exit 0, no commit.
func TestIntegration_WorkitemSweep_ZeroEligible(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-zero")
	createSweepWorkitem(t, tc, path, "design", "active-only")
	stampActiveRun(t, tc, path, "design", "active-only")
	commitFixture(t, tc, path)
	headBefore := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))

	res := runSweepJSON(t, tc, path)
	assert.Equal(t, 0, res.Candidates)
	assert.Empty(t, res.Items)
	assert.Equal(t, 0, res.Swept)

	headAfter := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))
	assert.Equal(t, headBefore, headAfter, "no commit when nothing to sweep")
}

// Scenario 2: one eligible design item -> moves to dungeon/completed, evidence
// on jsonl, commit created.
func TestIntegration_WorkitemSweep_SingleEligibleMovesAndCommits(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-single")
	createSweepWorkitem(t, tc, path, "design", "done-feature")
	stampCompletedRun(t, tc, path, "design", "done-feature")
	commitFixture(t, tc, path)
	headBefore := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))

	res := runSweepJSON(t, tc, path)
	require.Len(t, res.Items, 1)
	assert.Equal(t, 1, res.Swept)
	assert.Equal(t, 0, res.Failed)
	assert.Equal(t, "workflow_run_completed", res.Items[0].Evidence)
	assert.Equal(t, "r1", res.Items[0].ActiveRunID)
	assert.True(t, res.Items[0].Committed)
	assert.Contains(t, res.Items[0].To, "dungeon/completed")

	// Source gone, item now in a dated dungeon/completed bucket.
	exists, err := tc.CheckDirExists(path + "/workflow/design/done-feature")
	require.NoError(t, err)
	assert.False(t, exists, "source dir should be gone after sweep")
	found, err := checkDatedDungeonStatusItemExists(tc, path+"/workflow/design/dungeon/completed", "done-feature")
	require.NoError(t, err)
	assert.True(t, found, "item should be in a dated dungeon/completed bucket")

	// Evidence recorded and a commit created.
	audit, err := tc.ReadFile(path + "/.campaign/workitems/.workitems.jsonl")
	require.NoError(t, err)
	assert.Contains(t, audit, `"evidence":"workflow_run_completed"`)
	headAfter := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))
	assert.NotEqual(t, headBefore, headAfter, "sweep should create a commit")
}

// Scenario 3: one eligible + one ineligible (active) -> only eligible moves.
func TestIntegration_WorkitemSweep_EligibleAndIneligible(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-mixed")
	createSweepWorkitem(t, tc, path, "design", "ready-item")
	stampCompletedRun(t, tc, path, "design", "ready-item")
	createSweepWorkitem(t, tc, path, "design", "busy-item")
	stampActiveRun(t, tc, path, "design", "busy-item")
	commitFixture(t, tc, path)

	res := runSweepJSON(t, tc, path)
	assert.Equal(t, 1, res.Candidates, "only the completed-run item is a candidate")
	assert.Equal(t, 1, res.Swept)

	gone, err := tc.CheckDirExists(path + "/workflow/design/ready-item")
	require.NoError(t, err)
	assert.False(t, gone, "eligible item moved")
	stays, err := tc.CheckDirExists(path + "/workflow/design/busy-item")
	require.NoError(t, err)
	assert.True(t, stays, "active item stays put")
}

// Scenario 4: --dry-run mutates nothing and names the eligible item.
func TestIntegration_WorkitemSweep_DryRunNoMutation(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-dryrun")
	createSweepWorkitem(t, tc, path, "design", "done-feature")
	stampCompletedRun(t, tc, path, "design", "done-feature")
	commitFixture(t, tc, path)
	headBefore := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))

	res := runSweepJSON(t, tc, path, "--dry-run")
	assert.True(t, res.DryRun)
	require.Len(t, res.Items, 1)
	assert.Contains(t, res.Items[0].From, "workflow/design/done-feature")
	assert.Contains(t, res.Items[0].To, "dungeon/completed")

	exists, err := tc.CheckDirExists(path + "/workflow/design/done-feature")
	require.NoError(t, err)
	assert.True(t, exists, "dry-run must not move the item")
	headAfter := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))
	assert.Equal(t, headBefore, headAfter, "dry-run must not commit")
}

// Scenario 5: per-item failure isolation. The second item's dungeon resolution
// is made ambiguous (both spellings present), so its move fails while the first
// still moves and commits.
func TestIntegration_WorkitemSweep_PerItemErrorIsolation(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-isolation")
	createSweepWorkitem(t, tc, path, "design", "good-feature")
	stampCompletedRun(t, tc, path, "design", "good-feature")
	createSweepWorkitem(t, tc, path, "explore", "bad-feature")
	stampCompletedRun(t, tc, path, "explore", "bad-feature")
	require.NoError(t, tc.WriteFile(path+"/workflow/explore/dungeon/.keep", ""))
	require.NoError(t, tc.WriteFile(path+"/workflow/explore/.dungeon/.keep", ""))
	commitFixture(t, tc, path)

	res := runSweepJSON(t, tc, path)
	assert.Equal(t, 2, res.Candidates)
	assert.Equal(t, 1, res.Swept)
	assert.Equal(t, 1, res.Failed)

	var good, bad *sweepResultItem
	for i := range res.Items {
		if res.Items[i].Type == "design" {
			good = &res.Items[i]
		} else {
			bad = &res.Items[i]
		}
	}
	require.NotNil(t, good)
	require.NotNil(t, bad)
	assert.Empty(t, good.Error)
	assert.True(t, good.Committed)
	assert.NotEmpty(t, bad.Error, "conflicting item reports an error")

	gone, err := tc.CheckDirExists(path + "/workflow/design/good-feature")
	require.NoError(t, err)
	assert.False(t, gone, "healthy item moved")
	stays, err := tc.CheckDirExists(path + "/workflow/explore/bad-feature")
	require.NoError(t, err)
	assert.True(t, stays, "failed item stays put")
}

// Scenario 6: idempotency. Re-running immediately after a successful sweep finds
// nothing, because the swept item is now inside a dungeon that Discover skips.
func TestIntegration_WorkitemSweep_Idempotent(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-idempotent")
	createSweepWorkitem(t, tc, path, "design", "once-item")
	stampCompletedRun(t, tc, path, "design", "once-item")
	commitFixture(t, tc, path)

	first := runSweepJSON(t, tc, path)
	assert.Equal(t, 1, first.Swept)

	second := runSweepJSON(t, tc, path)
	assert.Equal(t, 0, second.Candidates, "swept item is no longer eligible")
	assert.Empty(t, second.Items)
}
