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

// Acceptance suite for CT0003 phase 006.
//
// These are the criteria PHASE_GOAL.md names, made mechanical: verify reports
// zero mismatches after every apply, the deterministic phases finish in
// seconds, and every rendered artifact is byte-reproducible from recorded run
// data. They run against fixture campaigns in the container, never a real one.
//
// They are kept as tests rather than a one-off transcript because an
// acceptance criterion that is only ever checked by hand stops being checked.

// phaseTiming is one deterministic phase's wall time.
type phaseTiming struct {
	Phase   string
	Elapsed time.Duration
}

// timePhase runs a camp command and records how long it took.
func timePhase(t *testing.T, tc *TestContainer, path, label string, args ...string) (string, phaseTiming) {
	t.Helper()
	started := time.Now()
	out, err := tc.RunCampInDir(path, args...)
	elapsed := time.Since(started)
	require.NoError(t, err, out)
	return out, phaseTiming{Phase: label, Elapsed: elapsed}
}

// TestAcceptance_VerifyReportsZeroMismatchesAfterApply is review criterion 1:
// `camp triage verify` reports zero unexplained mismatches after each apply.
//
// Checked after two applies, not one, because the criterion says "after each
// apply" — a verifier that only holds for a campaign's first run would pass a
// single-apply test and fail the claim.
func TestAcceptance_VerifyReportsZeroMismatchesAfterApply(t *testing.T) {
	tc := GetSharedContainer(t)
	// Directory-backed rows only. Intents cannot be applied at all today —
	// pinned by TestAcceptance_KnownBlocker_IntentRowsCannotBeApplied — so
	// including them here would make this test pass on a subset while
	// claiming the criterion holds.
	path := setupTriageCampaign(t, tc, "triage-acceptance-verify", 6, 0)

	_, runDir := startTriageRun(t, tc, path)
	first := manifestRowIDs(t, tc, runDir)
	require.Len(t, first, 6)
	approveAll(t, tc, path, first, "parked")

	applyOut, err := tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, applyOut)

	verifyOut, err := tc.RunCampInDir(path, "triage", "verify")
	require.NoError(t, err, verifyOut)
	t.Logf("\n===== VERIFY AFTER FIRST APPLY =====\n%s", verifyOut)
	assert.Contains(t, verifyOut, "6 checked, 6 matched, 0 mismatched")

	report := readVerification(t, tc, runDir)
	assert.Equal(t, 6, report.Totals.Checked)
	assert.Equal(t, 6, report.Totals.Matched)
	assert.Equal(t, 0, report.Totals.Mismatched)
	for _, row := range report.Rows {
		assert.Equal(t, "match", row.Result, "row %s", row.StableID)
	}

	// Second run, second apply: the criterion says "after each apply", and a
	// verifier that only held for a campaign's first run would pass a
	// single-apply test while failing the claim.
	out, err := tc.RunCampInDir(path, "triage", "start", "--full", "--json")
	require.NoError(t, err, out)
	secondDir := path + "/.campaign/triage/runs/" + decodeTriageStart(t, out)["run_id"].(string)
	approveAll(t, tc, path, manifestRowIDs(t, tc, secondDir), "parked")

	applyOut, err = tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, applyOut)

	verifyOut, err = tc.RunCampInDir(path, "triage", "verify")
	require.NoError(t, err, verifyOut)
	t.Logf("\n===== VERIFY AFTER SECOND APPLY =====\n%s", verifyOut)
	assert.Contains(t, verifyOut, "0 mismatched")

	second := readVerification(t, tc, secondDir)
	assert.Equal(t, 0, second.Totals.Mismatched,
		"verify must stay clean across applies, not only on a campaign's first run")
}

// verificationReport is `verification.json`.
type verificationReport struct {
	Rows []struct {
		StableID      string `json:"stable_id"`
		ExpectedStage string `json:"expected_stage"`
		Result        string `json:"result"`
	} `json:"rows"`
	Totals struct {
		Checked    int `json:"checked"`
		Matched    int `json:"matched"`
		Mismatched int `json:"mismatched"`
	} `json:"totals"`
}

func readVerification(t *testing.T, tc *TestContainer, runDir string) verificationReport {
	t.Helper()
	raw, err := tc.ReadFile(runDir + "/verification.json")
	require.NoError(t, err)
	var report verificationReport
	require.NoError(t, json.Unmarshal([]byte(raw), &report))
	return report
}

// TestAcceptance_RenderedDocumentsAreByteReproducible is review criterion 3 and
// CONFIGURABILITY acceptance 10: rendered artifacts are byte-reproducible from
// recorded run data.
//
// Re-rendering the same run must produce the same bytes. That is what makes the
// documents safe to overwrite in place rather than version — the D3 rule the
// export follows — and what lets a reader trust that a diff means the run
// changed rather than the renderer wandered.
func TestAcceptance_RenderedDocumentsAreByteReproducible(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-acceptance-bytes", 3, 1)

	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	for _, id := range ids {
		judgeRow(t, tc, path, id, "parked")
	}

	// The generated brief is written once by start and never re-rendered by a
	// command, so its stability is checked as "nothing else in the run touches
	// it" — the hash before and after a full render pass.
	driverBefore := sha256Of(t, tc, runDir+"/DRIVER.md")

	docs := []string{"TRIAGE_REVIEW.md", "PRIORITIES.md"}

	out, err := tc.RunCampInDir(path, "triage", "review")
	require.NoError(t, err, out)
	firstPass := map[string]string{}
	for _, doc := range docs {
		firstPass[doc] = sha256Of(t, tc, runDir+"/"+doc)
	}

	out, err = tc.RunCampInDir(path, "triage", "review")
	require.NoError(t, err, out)
	for _, doc := range docs {
		second := sha256Of(t, tc, runDir+"/"+doc)
		assert.Equal(t, firstPass[doc], second,
			"%s must be byte-identical across re-renders of the same run", doc)
		t.Logf("%-18s sha256 %s (stable across two renders)", doc, second)
	}

	// A third render after a no-op refresh: the run data did not change, so
	// neither may the documents.
	refreshOut, err := tc.RunCampInDir(path, "triage", "refresh", "--json")
	require.NoError(t, err, refreshOut)
	out, err = tc.RunCampInDir(path, "triage", "review")
	require.NoError(t, err, out)
	for _, doc := range docs {
		assert.Equal(t, firstPass[doc], sha256Of(t, tc, runDir+"/"+doc),
			"%s must be byte-identical after a refresh that changed nothing", doc)
	}

	driverAfter := sha256Of(t, tc, runDir+"/DRIVER.md")
	assert.Equal(t, driverBefore, driverAfter,
		"DRIVER.md is written once per run and must not drift under it")
	t.Logf("%-18s sha256 %s (unchanged across the run)", "DRIVER.md", driverAfter)
}

// Hashing reuses triage_refresh_test.go's sha256Of: it already hashes inside
// the container, which is the comparison that matters here — the bytes on
// disk, not anything the harness decoded on the way out.

// TestAcceptance_DeterministicPhasesCompleteInSeconds is review criterion 2 and
// CONFIGURABILITY acceptance 9: the deterministic phases are measured in
// seconds, per SCALE_AND_DETERMINISM's cost envelope.
//
// The wall times are logged whether or not they pass, because the criterion
// asks for them to be recorded as evidence, and a bound that is only reported
// when it holds is not evidence of anything.
func TestAcceptance_DeterministicPhasesCompleteInSeconds(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-acceptance-timing", 40, 20)

	var timings []phaseTiming

	startOut, timing := timePhase(t, tc, path, "start (snapshot 60 rows)", "triage", "start", "--json")
	timings = append(timings, timing)
	runID := decodeTriageStart(t, startOut)["run_id"].(string)
	runDir := path + "/.campaign/triage/runs/" + runID

	_, timing = timePhase(t, tc, path, "status", "triage", "status", "--json")
	timings = append(timings, timing)

	_, timing = timePhase(t, tc, path, "queue", "triage", "queue", "--json")
	timings = append(timings, timing)

	_, timing = timePhase(t, tc, path, "profile --resolved", "triage", "profile", "--resolved", "--json")
	timings = append(timings, timing)

	_, timing = timePhase(t, tc, path, "refresh (re-walk + reclassify)", "triage", "refresh", "--json")
	timings = append(timings, timing)

	ids := manifestRowIDs(t, tc, runDir)
	require.Len(t, ids, 60)
	for _, id := range ids[:5] {
		judgeRow(t, tc, path, id, "parked")
	}

	_, timing = timePhase(t, tc, path, "review (render both documents)", "triage", "review")
	timings = append(timings, timing)

	_, timing = timePhase(t, tc, path, "priorities", "triage", "priorities")
	timings = append(timings, timing)

	// An incremental second run over the same 60 rows: the steady-state case
	// acceptance 9 is about.
	_, timing = timePhase(t, tc, path, "abandon", "triage", "abandon", "--reason", "timing run")
	timings = append(timings, timing)
	_, timing = timePhase(t, tc, path, "start (incremental, 60 rows)", "triage", "start", "--json")
	timings = append(timings, timing)

	var report strings.Builder
	report.WriteString("\n===== DETERMINISTIC PHASE WALL TIMES (60-row fixture, container) =====\n")
	for _, tm := range timings {
		report.WriteString(fmt.Sprintf("  %-34s %8.3fs\n", tm.Phase, tm.Elapsed.Seconds()))
	}
	t.Log(report.String())

	for _, tm := range timings {
		assert.Less(t, tm.Elapsed.Seconds(), 10.0,
			"%s must complete in single-digit seconds (SCALE_AND_DETERMINISM cost envelope)", tm.Phase)
	}
}

// TestAcceptance_ZeroJudgmentRunEndToEnd is CONFIGURABILITY acceptance 10's
// second half: a model-free run works end to end.
//
// Nothing here consults a model. Evidence is submitted with --no-evidence,
// which is the honest "I read nothing, the row is obvious from its metadata"
// record, and the dispositions are a human's. If this passes, the loop does not
// depend on model assistance being available.
func TestAcceptance_ZeroJudgmentRunEndToEnd(t *testing.T) {
	tc := GetSharedContainer(t)
	// Directory-backed rows only, for the same reason as the verify test: an
	// intent row halts apply, and this test asserting "0 mismatched" would
	// then be asserting that nothing was checked.
	path := setupTriageCampaign(t, tc, "triage-acceptance-zerojudgment", 3, 0)

	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	require.Len(t, ids, 3)

	for _, id := range ids {
		out, err := tc.RunCampInDir(path, "triage", "evidence", "set", id, "--no-evidence", "--json")
		require.NoError(t, err, out)
		out, err = tc.RunCampInDir(path, "triage", "propose", id,
			"--disposition", "parked", "--summary", "no reading needed", "--json")
		require.NoError(t, err, out)
		out, err = tc.RunCampInDir(path, "triage", "approve", id, "--json")
		require.NoError(t, err, out)
	}

	out, err := tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, out)
	t.Logf("\n===== ZERO-JUDGMENT APPLY =====\n%s", out)
	assert.Contains(t, out, "Applied 3 row(s)",
		"every approved row must actually apply, not merely not error")
	assert.NotContains(t, out, "Stopped at",
		"a halted apply must not be mistaken for a completed one")

	out, err = tc.RunCampInDir(path, "triage", "verify")
	require.NoError(t, err, out)
	t.Logf("\n===== ZERO-JUDGMENT VERIFY =====\n%s", out)
	assert.Contains(t, out, "0 mismatched")

	status, err := tc.RunCampInDir(path, "triage", "status")
	require.NoError(t, err, status)
	assert.Contains(t, status, "verified",
		"a model-free run reaches the same terminal state as any other")
}

// TestAcceptance_UnconfiguredCampaignWithASmallBacklog is CONFIGURABILITY
// acceptance 1: `camp triage` works in a campaign nobody configured.
//
// The fixture never runs `triage init` and never writes a profile; start
// scaffolds one and proceeds. This is the first-run path a new user takes.
func TestAcceptance_UnconfiguredCampaignWithASmallBacklog(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-acceptance-unconfigured", 2, 1)

	exists, err := tc.CheckFileExists(path + "/.campaign/triage/profile.yaml")
	require.NoError(t, err)
	require.False(t, exists, "the fixture must start with no profile at all")

	out, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, out)
	payload := decodeTriageStart(t, out)
	assert.Equal(t, float64(3), payload["rows"])
	assert.Equal(t, "default", payload["profile"])

	exists, err = tc.CheckFileExists(path + "/.campaign/triage/profile.yaml")
	require.NoError(t, err)
	assert.True(t, exists,
		"the first run leaves behind the commented profile that explains what happened")
	exists, err = tc.CheckFileExists(path + "/.campaign/triage/OBEY.md")
	require.NoError(t, err)
	assert.True(t, exists, "and the guide acceptance 11 says a first triage is steered by")
}

// --- Known blockers, pinned -------------------------------------------
//
// These two assert behaviour that is wrong. They exist so the defects cannot
// be lost between this acceptance phase and whoever fixes them: the day the
// behaviour changes, these fail and point at the fix. Each names what the
// correct behaviour would be.

// TestAcceptance_KnownBlocker_IntentRowsCannotBeApplied pins the release
// blocker this phase found: `camp triage` cannot apply any verdict to an
// intent workitem.
//
// The plan compiler emits the row's stable id as the selector
// (`camp workitem stage intent-item-1 parked`). That resolves for a
// directory-backed workitem, whose id is its directory name, and fails for a
// file-backed one — camp answers "no workitem matched selector intent-item-1;
// did you mean: intent:.campaign/intents/inbox/intent-item-1.md". It shipped
// because every plan it was checked against held design rows, where the id and
// the selector coincide.
//
// All four labels the shipped intent policy offers fail this way. Intents are
// the idea inbox, which is the largest class in a real campaign and the thing
// the field trial that produced this design was triaging.
//
// Correct behaviour: an approved verdict on an intent applies, or triage
// refuses to offer a disposition it cannot execute. Either way `apply` must
// not report success having applied nothing.
func TestAcceptance_KnownBlocker_IntentRowsCannotBeApplied(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-blocker-intent-apply", 0, 1)

	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	require.Len(t, ids, 1)

	judgeRow(t, tc, path, ids[0], "ready")
	out, err := tc.RunCampInDir(path, "triage", "approve", ids[0], "--json")
	require.NoError(t, err, out)

	applyOut, err := tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, applyOut,
		"apply exits 0 even when it applied nothing — part of what hides this")
	t.Logf("\n===== BLOCKER: apply on an intent row =====\n%s", applyOut)

	assert.Contains(t, applyOut, "Applied 0 row(s)",
		"BLOCKER: no intent verdict can be applied")
	assert.Contains(t, applyOut, "Stopped at "+ids[0])

	receipts, err := tc.ReadFile(runDir + "/receipts.jsonl")
	require.NoError(t, err)
	assert.Contains(t, receipts, "no workitem matched selector "+ids[0],
		"the cause is the selector, recorded on the receipt")
	assert.Contains(t, receipts, `"result":"failed"`)
}

// TestAcceptance_KnownBlocker_PartialApplyStillVerifiesClean pins the defect
// that conceals the one above: a run whose apply halted partway still reaches
// `verified` with zero mismatches.
//
// `Verify` iterates the rows that produced an apply receipt with result
// `applied`. A row whose apply failed produced no such receipt, so it is never
// checked, and the report is clean by omission. `status` then reports
// `verified`, the terminal success state, while an approved decision sits
// unexecuted.
//
// Camp is honest about the *empty* case — an apply that moved nothing at all
// verifies with "Nothing has been applied yet, so there is nothing to prove."
// It is the partial case that misreports, and the partial case is the one a
// real campaign hits, because a campaign holds both designs and intents.
//
// The review criterion this phase is measured against reads "verify reports
// zero unexplained mismatches after each apply". A row that was approved and
// never applied is the definition of unexplained, and it is counted as neither.
//
// Correct behaviour: a run with an approved verdict that did not apply is not
// verified, and verify says which rows and why.
func TestAcceptance_KnownBlocker_PartialApplyStillVerifiesClean(t *testing.T) {
	tc := GetSharedContainer(t)
	// Two designs and one intent: apply moves the designs and halts on the
	// intent, which is exactly the shape of a real campaign's run.
	path := setupTriageCampaign(t, tc, "triage-blocker-partial-verify", 2, 1)

	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	require.Len(t, ids, 3)
	for _, id := range ids {
		judgeRow(t, tc, path, id, "parked")
		out, err := tc.RunCampInDir(path, "triage", "approve", id, "--json")
		require.NoError(t, err, out)
	}

	applyOut, err := tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, applyOut,
		"apply exits 0 despite halting — part of what hides this")
	t.Logf("\n===== BLOCKER: partial apply =====\n%s", applyOut)
	require.Contains(t, applyOut, "Applied 2 row(s)")
	require.Contains(t, applyOut, "Stopped at ")

	verifyOut, err := tc.RunCampInDir(path, "triage", "verify")
	require.NoError(t, err, verifyOut)
	t.Logf("\n===== BLOCKER: verify after a partial apply =====\n%s", verifyOut)

	assert.Contains(t, verifyOut, "2 checked, 2 matched, 0 mismatched",
		"BLOCKER: only the rows that applied are checked; the failed one is not counted")
	assert.Contains(t, verifyOut, "Every applied row is where its approved verdict said it would be",
		"BLOCKER: a clean bill of health that is true only of the subset that applied")

	report := readVerification(t, tc, runDir)
	assert.Equal(t, 2, report.Totals.Checked,
		"BLOCKER: 3 rows were approved, 2 are checked, and nothing reports the third")
	assert.Equal(t, 0, report.Totals.Mismatched)

	status, err := tc.RunCampInDir(path, "triage", "status")
	require.NoError(t, err, status)
	t.Logf("\n===== BLOCKER: status after a partial apply =====\n%s", status)
	assert.Contains(t, status, "verified",
		"BLOCKER: the run reaches its terminal success state with a decision unexecuted")
}
