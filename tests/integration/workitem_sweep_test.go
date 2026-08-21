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
	ID        string `json:"id"`
	Type      string `json:"type"`
	From      string `json:"from"`
	To        string `json:"to"`
	Evidence  string `json:"evidence"`
	RunID     string `json:"run_id"`
	Committed bool   `json:"committed"`
	Error     string `json:"error"`
}

// sweepSkipItem mirrors one entry of the additive "skipped" field: a workitem
// the sweep looked at and deliberately left where it was, with the reason.
type sweepSkipItem struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	From   string `json:"from"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// sweepReleasedLink mirrors one entry of the additive "released_links" field.
type sweepReleasedLink struct {
	ID        string `json:"id"`
	ScopeKind string `json:"scope_kind"`
	ScopePath string `json:"scope_path"`
	Workitem  string `json:"workitem"`
}

type sweepResult struct {
	SchemaVersion string              `json:"schema_version"`
	DryRun        bool                `json:"dry_run"`
	Candidates    int                 `json:"candidates"`
	Swept         int                 `json:"swept"`
	Failed        int                 `json:"failed"`
	Committed     bool                `json:"committed"`
	Items         []sweepResultItem   `json:"items"`
	Skipped       []sweepSkipItem     `json:"skipped"`
	ReleasedLinks []sweepReleasedLink `json:"released_links"`
}

// stampCompletedRun writes the REAL post-completion .workflow/ shape fest
// produces: active_run_id is CLEARED and the completed run survives in the
// manifest's runs[] index, its terminal status verifiable by replaying its
// events. This makes the workitem at <campaign>/workflow/<wkType>/<slug>
// eligible for tier-1 sweep. (Hand-written fixtures previously left active_run_id
// pointed at the completed run, a state fest never emits; that is the bug this
// PR fixes.)
func stampCompletedRun(t *testing.T, tc *TestContainer, campaignPath, wkType, slug string) {
	t.Helper()
	base := campaignPath + "/workflow/" + wkType + "/" + slug + "/.workflow"
	require.NoError(t, tc.WriteFile(base+"/workflow.yaml",
		"workflow_id: wf-"+slug+"\nruns:\n    - run_id: r1\n      status: completed\n      ended_at: \"2026-07-24T19:00:00Z\"\n"))
	require.NoError(t, tc.WriteFile(base+"/runs/r1/run.yaml", "status: completed\nsummary:\n  total_steps: 1\n"))
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

// backdateWorkitemContent ages the workitem's content files past the sweep's
// fresh-write window. A fixture is written milliseconds before the sweep runs,
// which is exactly the "a session is still writing here" shape the guard exists
// to catch, so every test that expects a move has to say the work is finished.
// .workflow/ is left alone deliberately: the guard ignores it, and touching it
// would hide a regression in that exclusion.
func backdateWorkitemContent(t *testing.T, tc *TestContainer, campaignPath, wkType, slug string) {
	t.Helper()
	dir := campaignPath + "/workflow/" + wkType + "/" + slug
	tc.Shell(t, "find "+dir+" -not -path '"+dir+"/.workflow*' -exec touch -d '2 hours ago' {} +")
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

// TestIntegration_WorkitemSweep_EndToEndFestDrivenCompletion is the acceptance
// bar for the latest-run eligibility fix: it drives the REAL fest binary to
// complete a standalone loop (so active_run_id is genuinely cleared, the shape
// hand-written fixtures never modeled), then asserts camp workitem sweep
// actually promotes the completed workitem.
func TestIntegration_WorkitemSweep_EndToEndFestDrivenCompletion(t *testing.T) {
	if !festAvailable {
		t.Skip("fest binary not available in container; skipping fest-driven e2e sweep test")
	}
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-e2e-fest")
	createSweepWorkitem(t, tc, path, "chore", "e2e-feature")
	wi := path + "/workflow/chore/e2e-feature"

	// Drive a real fest standalone loop (1 step) to completion.
	_, _, err := tc.ExecCommand("sh", "-c",
		"cd "+wi+" && fest create workflow e2e --steps '{\"title\":\"E2E\",\"steps\":[{\"name\":\"s1\",\"goal\":\"g1\"}]}'")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+wi+" && fest workflow advance")
	require.NoError(t, err)

	// Real post-completion shape: fest clears active_run_id on completion.
	wf, err := tc.ReadFile(wi + "/.workflow/workflow.yaml")
	require.NoError(t, err)
	assert.NotContains(t, wf, "active_run_id:",
		"fest must clear active_run_id on completion (the shape the sweep now keys on):\n%s", wf)

	backdateWorkitemContent(t, tc, path, "chore", "e2e-feature")
	commitFixture(t, tc, path)

	// The sweep must fire on the genuinely fest-completed workitem.
	res := runSweepJSON(t, tc, path)
	assert.Equal(t, 1, res.Swept, "a fest-completed workitem must be swept: %+v", res)
	require.Len(t, res.Items, 1)
	assert.Equal(t, "workflow_run_completed", res.Items[0].Evidence)
	assert.NotEmpty(t, res.Items[0].RunID, "the completed run's id must be recorded as evidence")

	exists, err := tc.CheckDirExists(wi)
	require.NoError(t, err)
	assert.False(t, exists, "swept item's source dir should be gone (moved to the dungeon)")
}

// Scenario 1: zero eligible items -> empty result, exit 0, no commit.
func TestIntegration_WorkitemSweep_ZeroEligible(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-zero")
	createSweepWorkitem(t, tc, path, "chore", "active-only")
	stampActiveRun(t, tc, path, "chore", "active-only")
	commitFixture(t, tc, path)
	headBefore := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))

	res := runSweepJSON(t, tc, path)
	assert.Equal(t, 0, res.Candidates)
	assert.Empty(t, res.Items)
	assert.Equal(t, 0, res.Swept)

	headAfter := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))
	assert.Equal(t, headBefore, headAfter, "no commit when nothing to sweep")
}

// Scenario 2: one eligible chore item -> moves to dungeon/completed, evidence
// on jsonl, commit created.
func TestIntegration_WorkitemSweep_SingleEligibleMovesAndCommits(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-single")
	createSweepWorkitem(t, tc, path, "chore", "done-feature")
	stampCompletedRun(t, tc, path, "chore", "done-feature")
	backdateWorkitemContent(t, tc, path, "chore", "done-feature")
	commitFixture(t, tc, path)
	headBefore := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))

	res := runSweepJSON(t, tc, path)
	require.Len(t, res.Items, 1)
	assert.Equal(t, 1, res.Swept)
	assert.Equal(t, 0, res.Failed)
	assert.Equal(t, "workflow_run_completed", res.Items[0].Evidence)
	assert.Equal(t, "r1", res.Items[0].RunID)
	assert.True(t, res.Items[0].Committed)
	assert.Contains(t, res.Items[0].To, "dungeon/completed")

	// Source gone, item now in a dated dungeon/completed bucket.
	exists, err := tc.CheckDirExists(path + "/workflow/chore/done-feature")
	require.NoError(t, err)
	assert.False(t, exists, "source dir should be gone after sweep")
	found, err := checkDatedDungeonStatusItemExists(tc, path+"/workflow/chore/dungeon/completed", "done-feature")
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
	createSweepWorkitem(t, tc, path, "chore", "ready-item")
	stampCompletedRun(t, tc, path, "chore", "ready-item")
	backdateWorkitemContent(t, tc, path, "chore", "ready-item")
	createSweepWorkitem(t, tc, path, "chore", "busy-item")
	stampActiveRun(t, tc, path, "chore", "busy-item")
	backdateWorkitemContent(t, tc, path, "chore", "busy-item")
	commitFixture(t, tc, path)

	res := runSweepJSON(t, tc, path)
	assert.Equal(t, 1, res.Candidates, "only the completed-run item is a candidate")
	assert.Equal(t, 1, res.Swept)

	gone, err := tc.CheckDirExists(path + "/workflow/chore/ready-item")
	require.NoError(t, err)
	assert.False(t, gone, "eligible item moved")
	stays, err := tc.CheckDirExists(path + "/workflow/chore/busy-item")
	require.NoError(t, err)
	assert.True(t, stays, "active item stays put")
}

// Scenario 4: --dry-run mutates nothing and names the eligible item.
func TestIntegration_WorkitemSweep_DryRunNoMutation(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-dryrun")
	createSweepWorkitem(t, tc, path, "chore", "done-feature")
	stampCompletedRun(t, tc, path, "chore", "done-feature")
	backdateWorkitemContent(t, tc, path, "chore", "done-feature")
	commitFixture(t, tc, path)
	headBefore := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))

	res := runSweepJSON(t, tc, path, "--dry-run")
	assert.True(t, res.DryRun)
	require.Len(t, res.Items, 1)
	assert.Contains(t, res.Items[0].From, "workflow/chore/done-feature")
	assert.Contains(t, res.Items[0].To, "dungeon/completed")

	exists, err := tc.CheckDirExists(path + "/workflow/chore/done-feature")
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
	createSweepWorkitem(t, tc, path, "chore", "good-feature")
	stampCompletedRun(t, tc, path, "chore", "good-feature")
	backdateWorkitemContent(t, tc, path, "chore", "good-feature")
	createSweepWorkitem(t, tc, path, "bug", "bad-feature")
	stampCompletedRun(t, tc, path, "bug", "bad-feature")
	backdateWorkitemContent(t, tc, path, "bug", "bad-feature")
	require.NoError(t, tc.WriteFile(path+"/workflow/bug/dungeon/.keep", ""))
	require.NoError(t, tc.WriteFile(path+"/workflow/bug/.dungeon/.keep", ""))
	commitFixture(t, tc, path)

	res := runSweepJSON(t, tc, path)
	assert.Equal(t, 2, res.Candidates)
	assert.Equal(t, 1, res.Swept)
	assert.Equal(t, 1, res.Failed)

	var good, bad *sweepResultItem
	for i := range res.Items {
		if res.Items[i].Type == "chore" {
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

	gone, err := tc.CheckDirExists(path + "/workflow/chore/good-feature")
	require.NoError(t, err)
	assert.False(t, gone, "healthy item moved")
	stays, err := tc.CheckDirExists(path + "/workflow/bug/bad-feature")
	require.NoError(t, err)
	assert.True(t, stays, "failed item stays put")
}

// Scenario 6: idempotency. Re-running immediately after a successful sweep finds
// nothing, because the swept item is now inside a dungeon that Discover skips.
func TestIntegration_WorkitemSweep_Idempotent(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-idempotent")
	createSweepWorkitem(t, tc, path, "chore", "once-item")
	stampCompletedRun(t, tc, path, "chore", "once-item")
	backdateWorkitemContent(t, tc, path, "chore", "once-item")
	commitFixture(t, tc, path)

	first := runSweepJSON(t, tc, path)
	assert.Equal(t, 1, first.Swept)

	second := runSweepJSON(t, tc, path)
	assert.Equal(t, 0, second.Candidates, "swept item is no longer eligible")
	assert.Empty(t, second.Items)
}

// writeStaleScopedLink writes a links.yaml row scoped at the workitem directory
// whose workitem_id and workitem_key both name a DIFFERENT workitem, which is
// the on-disk state a directory rename leaves behind: the link was made while
// the directory had another name, so its recorded identity no longer matches the
// one discovery derives today. This is the exact registry the 2026-08-20 sweep
// walked past, leaving two broken links behind (campaign commit 6dc51fab8).
func writeStaleScopedLink(t *testing.T, tc *TestContainer, campaignPath, linkID, scopePath string) {
	t.Helper()
	require.NoError(t, tc.WriteFile(campaignPath+"/.campaign/workitems/links.yaml",
		"version: workitem-links/v1alpha1\n"+
			"links:\n"+
			"    - id: "+linkID+"\n"+
			"      workitem_id: explore-renamed-under-another-name-2026-08-20\n"+
			"      workitem_key: explore:workflow/explore/renamed-under-another-name\n"+
			"      scope:\n"+
			"        kind: campaign_path\n"+
			"        path: "+scopePath+"\n"+
			"      role: primary\n"+
			"      created_at: 2026-08-20T13:00:00Z\n"+
			"      created_by: integration-test\n"))
}

// Scenario 7: promoting a workitem releases a link scoped INSIDE its directory
// in the same commit as the move, even though the link names a workitem id the
// directory no longer answers to. Before this fix promote matched links by id or
// key only, so a renamed directory's older links survived the move and camp
// status reported them as broken until camp workitem doctor --fix removed them
// by hand (campaign commits 6dc51fab8 and 871818db4, 2026-08-20).
//
// Driven through camp workitem promote rather than the automatic sweep on
// purpose: an explicit promote is a direct instruction with no guards, while the
// automatic sweep now declines to move a linked directory at all (scenario 11).
func TestIntegration_WorkitemPromote_ReleasesPathScopedLinkAfterRename(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "promote-stale-link")
	createSweepWorkitem(t, tc, path, "chore", "renamed-item")
	writeStaleScopedLink(t, tc, path, "lnk_20260820_6c045e", "workflow/chore/renamed-item")
	commitFixture(t, tc, path)

	out, err := tc.RunCampInDir(path, "workitem", "promote", "chore-renamed-item-fixed",
		"--target", "completed", "--json")
	require.NoError(t, err, "promote: %s", out)

	var promoted struct {
		ReleasedLinks []sweepReleasedLink `json:"released_links"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &promoted), "must parse: %s", out)
	require.Len(t, promoted.ReleasedLinks, 1, "the path-scoped link must be reported as released: %s", out)
	assert.Equal(t, "lnk_20260820_6c045e", promoted.ReleasedLinks[0].ID)
	assert.Equal(t, "workflow/chore/renamed-item", promoted.ReleasedLinks[0].ScopePath)

	registry, err := tc.ReadFile(path + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.NotContains(t, registry, "lnk_20260820_6c045e", "the orphaned link must be gone from the registry")

	// Same commit: nothing about the move is left uncommitted afterward.
	assert.Empty(t, strings.TrimSpace(tc.GitOutput(t, path, "status", "--porcelain")),
		"the move and the link release must land in one commit")
	assert.Contains(t, tc.GitOutput(t, path, "show", "--stat", "--name-only", "HEAD"),
		".campaign/workitems/links.yaml", "links.yaml must be part of the move's commit")
}

// Scenario 8: a directory written within the fresh-write window is left alone and
// the reason is reported. On 2026-08-17 the sweep moved an explore directory that
// three agents were still writing into; their later writes landed in an orphaned
// untracked directory at the old path.
func TestIntegration_WorkitemSweep_SkipsFreshlyWrittenDirectory(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-fresh-writes")
	createSweepWorkitem(t, tc, path, "chore", "busy-item")
	stampCompletedRun(t, tc, path, "chore", "busy-item")
	commitFixture(t, tc, path)

	// A live session writes into the directory right now.
	require.NoError(t, tc.WriteFile(path+"/workflow/chore/busy-item/NOTES.md", "still writing\n"))

	res := runSweepJSON(t, tc, path)
	assert.Equal(t, 0, res.Swept, "a directory with a live writer must not move: %+v", res)
	require.Len(t, res.Skipped, 1, "the non-move must be reported: %+v", res)
	assert.Equal(t, "recent_writes", res.Skipped[0].Reason)
	assert.Contains(t, res.Skipped[0].From, "workflow/chore/busy-item")
	assert.Contains(t, res.Skipped[0].Detail, "camp workitem promote",
		"the report must name the command that overrules the guard")

	stays, err := tc.CheckDirExists(path + "/workflow/chore/busy-item")
	require.NoError(t, err)
	assert.True(t, stays, "the freshly written item must stay put")
}

// Scenario 9: a design with a completed run is never moved by run-completion
// evidence. A design is done when it is implemented, so the report says what
// evidence would promote it instead of burying the specification.
func TestIntegration_WorkitemSweep_DesignIsNeverPromotedByRunCompletion(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-design-hold")
	createSweepWorkitem(t, tc, path, "design", "spec-item")
	stampCompletedRun(t, tc, path, "design", "spec-item")
	backdateWorkitemContent(t, tc, path, "design", "spec-item")
	commitFixture(t, tc, path)

	res := runSweepJSON(t, tc, path)
	assert.Equal(t, 0, res.Candidates, "a design is not a sweep candidate: %+v", res)
	assert.Equal(t, 0, res.Swept)
	require.Len(t, res.Skipped, 1, "the design must be reported, not silently ignored: %+v", res)
	assert.Equal(t, "design_awaits_implementation", res.Skipped[0].Reason)
	assert.Contains(t, res.Skipped[0].Detail, "implementation evidence")

	stays, err := tc.CheckDirExists(path + "/workflow/design/spec-item")
	require.NoError(t, err)
	assert.True(t, stays, "a design must not be moved by run-completion evidence")
}

// Scenario 10: explore findings are never moved automatically. The explicit
// command still promotes an eligible chore alongside it, and reports the explore
// item's non-move with the command that can answer the routing question.
func TestIntegration_WorkitemSweep_ExploreReportedChorePromoted(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-explore-report")
	createSweepWorkitem(t, tc, path, "chore", "done-chore")
	stampCompletedRun(t, tc, path, "chore", "done-chore")
	backdateWorkitemContent(t, tc, path, "chore", "done-chore")
	createSweepWorkitem(t, tc, path, "explore", "findings")
	stampCompletedRun(t, tc, path, "explore", "findings")
	backdateWorkitemContent(t, tc, path, "explore", "findings")
	commitFixture(t, tc, path)

	res := runSweepJSON(t, tc, path)
	assert.Equal(t, 1, res.Swept, "the chore must still promote automatically: %+v", res)
	require.Len(t, res.Items, 1)
	assert.Contains(t, res.Items[0].From, "workflow/chore/done-chore")

	require.Len(t, res.Skipped, 1, "the explore item's non-move must be reported: %+v", res)
	assert.Equal(t, "needs_routing", res.Skipped[0].Reason)
	assert.Contains(t, res.Skipped[0].Detail, "camp workitem sweep --prompt")

	stays, err := tc.CheckDirExists(path + "/workflow/explore/findings")
	require.NoError(t, err)
	assert.True(t, stays, "findings must never be buried automatically")
}

// Scenario 11: a linked directory is not moved out from under whoever holds the
// link while the sweep is acting on its own. The prompt is where that decision
// belongs, so the reported reason names it.
func TestIntegration_WorkitemSweep_AutoModeSkipsLinkedDirectory(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-linked-hold")
	createSweepWorkitem(t, tc, path, "chore", "linked-item")
	stampCompletedRun(t, tc, path, "chore", "linked-item")
	backdateWorkitemContent(t, tc, path, "chore", "linked-item")
	writeStaleScopedLink(t, tc, path, "lnk_20260820_c312f7", "workflow/chore/linked-item")
	commitFixture(t, tc, path)

	res := runSweepJSON(t, tc, path)
	assert.Equal(t, 0, res.Swept, "a linked directory must not be swept automatically: %+v", res)
	require.Len(t, res.Skipped, 1, "the non-move must be reported: %+v", res)
	assert.Equal(t, "linked_scope", res.Skipped[0].Reason)
	assert.Contains(t, res.Skipped[0].Detail, "lnk_20260820_c312f7")
	assert.Contains(t, res.Skipped[0].Detail, "camp workitem sweep --prompt")

	stays, err := tc.CheckDirExists(path + "/workflow/chore/linked-item")
	require.NoError(t, err)
	assert.True(t, stays, "a reported item must stay put")

	registry, err := tc.ReadFile(path + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, registry, "lnk_20260820_c312f7", "a skipped move must not release the link")
}

// Scenario 12: --prompt with no terminal reports instead of blocking on a form
// nobody can see, so an agent's run never takes an automatic path.
func TestIntegration_WorkitemSweep_PromptWithoutTerminalReports(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "sweep-prompt-notty")
	createSweepWorkitem(t, tc, path, "chore", "done-chore")
	stampCompletedRun(t, tc, path, "chore", "done-chore")
	backdateWorkitemContent(t, tc, path, "chore", "done-chore")
	commitFixture(t, tc, path)
	headBefore := strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD"))

	out, err := tc.RunCampInDir(path, "workitem", "sweep", "--prompt")
	require.NoError(t, err, "sweep --prompt: %s", out)
	assert.Contains(t, out, "completed runs; run camp workitem sweep --prompt")

	stays, err := tc.CheckDirExists(path + "/workflow/chore/done-chore")
	require.NoError(t, err)
	assert.True(t, stays, "a prompt with nobody to answer must not move anything")
	assert.Equal(t, headBefore, strings.TrimSpace(tc.GitOutput(t, path, "rev-parse", "HEAD")),
		"report mode must not commit")
}
