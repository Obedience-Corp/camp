//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// incrementalManifest is the parts of a snapshot the carry story is about.
type incrementalManifest struct {
	BaseRunID   *string `json:"base_run_id"`
	CarryLosses []struct {
		StableID string `json:"stable_id"`
		Reason   string `json:"reason"`
	} `json:"carry_losses"`
	Rows []struct {
		StableID    string  `json:"stable_id"`
		CarriedFrom *string `json:"carried_from"`
	} `json:"rows"`
}

func readIncrementalManifest(t *testing.T, tc *TestContainer, runDir string) incrementalManifest {
	t.Helper()
	raw, err := tc.ReadFile(runDir + "/manifest.json")
	require.NoError(t, err)
	var manifest incrementalManifest
	require.NoError(t, json.Unmarshal([]byte(raw), &manifest))
	return manifest
}

// decideEveryRow drives a run all the way to verified, so the next start has
// real closed decisions to carry rather than a run that merely exists.
//
// Verify is not optional here: it is what makes the phase terminal, and start
// refuses while a run is still in progress. A test that stopped at apply would
// be testing the refusal.
func decideEveryRow(t *testing.T, tc *TestContainer, path, runDir, disposition string) []string {
	t.Helper()
	ids := manifestRowIDs(t, tc, runDir)
	approveAll(t, tc, path, ids, disposition)
	applyJSON(t, tc, path)
	out, err := tc.RunCampInDir(path, "triage", "verify", "--json")
	require.NoError(t, err, out)
	return ids
}

// TestTriageIncremental_SecondStartCarriesApprovedVerdicts is D4 end to end:
// the second triage of a campaign nothing has touched should have nothing to
// judge.
func TestTriageIncremental_SecondStartCarriesApprovedVerdicts(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-incremental-carry", 3, 0)

	firstID, firstDir := startTriageRun(t, tc, path)
	ids := decideEveryRow(t, tc, path, firstDir, "parked")
	require.Len(t, ids, 3)

	out, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, out)
	payload := decodeTriageStart(t, out)

	assert.Equal(t, float64(3), payload["carried"], "nothing moved, so every verdict carries")
	assert.Equal(t, float64(0), payload["queued"])
	assert.Equal(t, firstID, payload["base_run_id"])
	assert.Equal(t, "incremental", payload["mode"],
		"a run that carried from a base run is incremental, and the manifest has to say so")
	assert.Empty(t, payload["carry_losses"], "no row lost a carry: %v", payload["carry_losses"])

	secondDir := path + "/.campaign/triage/runs/" + payload["run_id"].(string)
	manifest := readIncrementalManifest(t, tc, secondDir)
	require.NotNil(t, manifest.BaseRunID)
	assert.Equal(t, firstID, *manifest.BaseRunID)
	for _, row := range manifest.Rows {
		require.NotNil(t, row.CarriedFrom, "row %s should be marked carried", row.StableID)
		assert.Equal(t, firstID, *row.CarriedFrom)
	}

	// The point of carrying: the queue is empty, so a driver has no work.
	queueOut, err := tc.RunCampInDir(path, "triage", "queue", "--json")
	require.NoError(t, err, queueOut)
	assert.Empty(t, queueItems(t, queueOut),
		"a run that carried everything asks nobody to judge anything")
}

// TestTriageIncremental_FullForcesReReview: --full is the escape hatch, and it
// has to actually re-queue rather than quietly carry.
func TestTriageIncremental_FullForcesReReview(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-incremental-full", 3, 0)

	_, firstDir := startTriageRun(t, tc, path)
	decideEveryRow(t, tc, path, firstDir, "parked")

	out, err := tc.RunCampInDir(path, "triage", "start", "--full", "--json")
	require.NoError(t, err, out)
	payload := decodeTriageStart(t, out)

	assert.Equal(t, float64(0), payload["carried"])
	assert.Equal(t, float64(3), payload["queued"])
	assert.Equal(t, "full", payload["mode"])
	assert.Empty(t, payload["base_run_id"], "a full run diffs against nothing")
}

// TestTriageIncremental_UnapprovedProposalDoesNotCarry is the regression for a
// proposal that carried forward into a run that could no longer act on it.
//
// A carried row leaves the queue and the review gate, and the new run holds no
// verdict for it, so a proposal nobody had approved yet stopped being visible
// to anyone: never re-judged, never approved, never applied. It must re-queue,
// and the manifest must say why.
func TestTriageIncremental_UnapprovedProposalDoesNotCarry(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-incremental-unapproved", 2, 0)

	_, firstDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, firstDir)
	require.Len(t, ids, 2)

	// Proposed, deliberately not approved: this is a row still waiting on a
	// human when the operator starts the next run.
	proposeRow(t, tc, path, ids[0], "parked")
	out, err := tc.RunCampInDir(path, "triage", "abandon", "--reason", "next run", "--json")
	require.NoError(t, err, out)

	out, err = tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, out)
	payload := decodeTriageStart(t, out)

	assert.Equal(t, float64(0), payload["carried"], "an unapproved proposal is not a decision")
	assert.Equal(t, float64(2), payload["queued"])

	losses, ok := payload["carry_losses"].([]any)
	require.True(t, ok, "carry_losses must always be an array: %s", out)
	require.Len(t, losses, 1, "the proposed row is the only one that held anything")
	loss := losses[0].(map[string]any)
	assert.Equal(t, ids[0], loss["stable_id"])
	assert.Contains(t, loss["reason"], "never approved")

	// The row is back in front of a judge rather than silently decided.
	queueOut, err := tc.RunCampInDir(path, "triage", "queue", "--json")
	require.NoError(t, err, queueOut)
	queued := make([]string, 0, 2)
	for _, item := range queueItems(t, queueOut) {
		queued = append(queued, item["stable_id"].(string))
	}
	assert.Contains(t, queued, ids[0])
}

// TestTriageIncremental_RefreshDoesNotLoseEveryCarriedRow is the regression
// for re-deciding a carried verdict against the wrong run.
//
// A carried row's verdict lives in the run that decided it; the new run
// records only that it stands. Looking for it in the current run finds nothing
// and reports every carried row as a lost carry — turning the feature's
// success case into a page of spurious losses on the very next refresh.
func TestTriageIncremental_RefreshDoesNotLoseEveryCarriedRow(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-incremental-refresh", 3, 0)

	_, firstDir := startTriageRun(t, tc, path)
	decideEveryRow(t, tc, path, firstDir, "parked")

	out, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, out)
	require.Equal(t, float64(3), decodeTriageStart(t, out)["carried"])

	result := refresh(t, tc, path)

	assert.Empty(t, result.CarryLost,
		"nothing moved between the two runs, so no carry was lost: %+v", result.CarryLost)
	assert.Equal(t, 0, result.Summary.CarryLost)
	assert.Equal(t, 0, result.Summary.StaleRecorded)
}

// TestTriageIncremental_StatusExplainsAStartTimeLoss is spec doc 04's
// requirement that `camp triage status --json` answer why a row is being
// judged again — including a row re-queued when the run was built, whose
// reason existed nowhere but the start command's output.
func TestTriageIncremental_StatusExplainsAStartTimeLoss(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-incremental-status", 2, 0)

	_, firstDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, firstDir)
	proposeRow(t, tc, path, ids[0], "parked")
	out, err := tc.RunCampInDir(path, "triage", "abandon", "--reason", "next run", "--json")
	require.NoError(t, err, out)

	out, err = tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, out)

	statusOut, err := tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, statusOut)
	losses, ok := decodeTriageJSON(t, statusOut)["run"].(map[string]any)["carry_losses"].([]any)
	require.True(t, ok, "status must always carry a carry_losses array: %s", statusOut)
	require.Len(t, losses, 1)
	assert.Equal(t, ids[0], losses[0].(map[string]any)["stable_id"])
	assert.Contains(t, losses[0].(map[string]any)["reason"], "never approved")
}

// TestTriageIncremental_TextOutputReportsWhatItReused is the human half of the
// command contract, which says start "reports carried/queued counts".
//
// Reusing a verdict is camp deciding on the operator's behalf that a row does
// not need looking at. Reporting that only under --json means the default
// invocation makes the decision silently.
func TestTriageIncremental_TextOutputReportsWhatItReused(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-incremental-text", 2, 0)

	firstID, firstDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, firstDir)
	proposeRow(t, tc, path, ids[0], "parked")
	judgeRow(t, tc, path, ids[1], "parked")
	out, err := tc.RunCampInDir(path, "triage", "approve", ids[1], "--json")
	require.NoError(t, err, out)

	out, err = tc.RunCampInDir(path, "triage", "abandon", "--reason", "next run", "--json")
	require.NoError(t, err, out)

	text, err := tc.RunCampInDir(path, "triage", "start")
	require.NoError(t, err, text)

	assert.Contains(t, text, "1 carried forward from "+firstID)
	assert.Contains(t, text, "1 queued for judgment")
	assert.Contains(t, text, "held a verdict that did not carry")
	assert.Contains(t, text, ids[0], "the row that lost its carry is named")
	assert.Contains(t, text, "never approved", "with the reason it lost it")
	assert.Contains(t, text, "brief: ", "the generated agent brief is worth naming")
}

// TestTriageIncremental_BootstrapSaysNothingAboutCarrying: a first run has
// nothing to reuse, and a line reporting "0 carried" would be noise on the one
// invocation where it can never mean anything.
func TestTriageIncremental_BootstrapSaysNothingAboutCarrying(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-incremental-bootstrap", 2, 0)

	text, err := tc.RunCampInDir(path, "triage", "start")
	require.NoError(t, err, text)

	assert.NotContains(t, text, "carried forward")
	assert.Contains(t, text, "2 rows in")
}

// TestTriageNotice_HonorsTheCampaignThreshold is the regression for a banner
// that read camp's built-in threshold rather than the campaign's.
//
// The scaffolded profile documents `runs.stale_after_days` as the `camp
// status` notice threshold. A campaign that raises it and still gets nagged
// has a key it can set and camp ignores, which is exactly the failure the
// strict profile decoder exists to prevent one layer down.
func TestTriageNotice_HonorsTheCampaignThreshold(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-notice-threshold", 2, 0)

	runID, _ := startTriageRun(t, tc, path)
	refresh(t, tc, path)

	// Age the cached verdict rather than the clock: the notice is a function
	// of what the last refresh saw and when, and this is the only part of that
	// pair a test can move.
	notice := `{"schema_version":"triage/v1alpha1","run_id":"` + runID +
		`","checked_at":"2026-01-01T00:00:00Z","changed_rows":0}`
	require.NoError(t, tc.WriteFile(path+"/.campaign/triage/notice.json", notice))

	out, err := tc.RunCampInDir(path, "status")
	require.NoError(t, err, out)
	assert.Contains(t, out, "run: camp triage start",
		"the default 14-day threshold should fire on a run this old")

	tc.Shell(t, "set -e\ncd "+path+
		"\nsed -i 's/^  stale_after_days: .*/  stale_after_days: 20000/' .campaign/triage/profile.yaml")

	out, err = tc.RunCampInDir(path, "status")
	require.NoError(t, err, out)
	assert.NotContains(t, out, "camp triage start",
		"a campaign that raised the threshold past this run's age must go quiet")
}

// TestTriageNotice_SaysNothingWithoutARun keeps the banner off the hot path of
// every campaign that has never triaged, which is most of them.
func TestTriageNotice_SaysNothingWithoutARun(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-notice-silent", 2, 0)

	for _, args := range [][]string{
		{"status"},
		{"workitem", "--list"},
		{"workitem", "sweep"},
		{"triage", "status"},
	} {
		out, err := tc.RunCampInDir(path, args...)
		require.NoError(t, err, "%v: %s", args, out)
		assert.NotContains(t, out, "last triage was", args)
		assert.NotContains(t, out, "changed since the last triage", args)
	}
}

// TestTriageNotice_ReachesSweepBannerSurfacesAndTriageStatus is the follow-up
// to CT0003 S3: the notice shipped on camp status only. It must also print from
// the SweepBannerText surfaces and from camp triage status (doc 03).
func TestTriageNotice_ReachesSweepBannerSurfacesAndTriageStatus(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-notice-surfaces", 2, 0)

	runID, _ := startTriageRun(t, tc, path)
	refresh(t, tc, path)
	notice := `{"schema_version":"triage/v1alpha1","run_id":"` + runID +
		`","checked_at":"2026-01-01T00:00:00Z","changed_rows":0}`
	require.NoError(t, tc.WriteFile(path+"/.campaign/triage/notice.json", notice))

	const want = "run: camp triage start"
	for _, args := range [][]string{
		{"status"},
		{"workitem", "--list"},
		{"workitem", "sweep"},
		{"triage", "status"},
	} {
		out, err := tc.RunCampInDir(path, args...)
		require.NoError(t, err, "%v: %s", args, out)
		assert.Contains(t, out, want, args)
	}

	jsonOut, err := tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, jsonOut)
	payload := decodeTriageJSON(t, jsonOut)
	got, _ := payload["stale_notice"].(string)
	assert.Contains(t, got, want)
}
