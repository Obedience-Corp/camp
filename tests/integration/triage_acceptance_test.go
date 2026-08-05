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
		Unapplied  int `json:"unapplied"`
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
	path := setupTriageCampaign(t, tc, "triage-acceptance-bytes", 3, 0)

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

// --- Blocker 1 still open; blocker 2's reporting half fixed -----------

// TestAcceptance_MixedRunAppliesIntentsAndDesigns is blocker 1 fixed: a run
// holding both workitem kinds applies every approved verdict.
//
// Camp's lifecycle verbs split along the directory/file line. `camp workitem
// stage` refuses a file-backed item outright and `camp workitem promote`
// cannot resolve one by id, so every verdict on an intent used to fail at
// apply time — all four labels its policy offered, on the largest class in a
// real campaign.
//
// The manifest now records `item_kind`, the compiler emits a distinct command
// kind for file-backed rows, and the executor routes that kind to camp's idea
// service. The argv and the executor agree, which they would not have if only
// the rendered command had changed.
func TestAcceptance_MixedRunAppliesIntentsAndDesigns(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-acceptance-mixed", 2, 2)

	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	require.Len(t, ids, 4)

	manifest, err := tc.ReadFile(runDir + "/manifest.json")
	require.NoError(t, err)
	assert.Contains(t, manifest, `"item_kind": "directory"`)
	assert.Contains(t, manifest, `"item_kind": "file"`,
		"the compiler is pure over the snapshot, so the snapshot carries the backing kind")

	for _, id := range ids {
		// Each type's own vocabulary: a design parks, an idea defers.
		disposition := "parked"
		if strings.HasPrefix(id, "intent-") {
			disposition = "someday"
		}
		judgeRow(t, tc, path, id, disposition)
		out, approveErr := tc.RunCampInDir(path, "triage", "approve", id, "--json")
		require.NoError(t, approveErr, out)
	}

	applyOut, applyErr := tc.RunCampInDir(path, "triage", "apply")
	t.Logf("\n===== MIXED APPLY =====\n%s", applyOut)
	require.NoError(t, applyErr, applyOut)
	assert.Contains(t, applyOut, "Applied 4 row(s)")
	assert.NotContains(t, applyOut, "Stopped at")
	assert.Contains(t, applyOut, "camp idea move",
		"a file-backed row compiles to the idea lifecycle verb")
	assert.Contains(t, applyOut, "camp workitem stage",
		"and a directory-backed row still compiles to the workitem verb")

	// The ideas actually moved on disk, which is the claim that matters.
	moved := tc.Shell(t, "cd "+path+" && find .campaign/intents -name 'intent-item-*.md' | sort")
	t.Logf("intents now at:\n%s", moved)
	assert.Contains(t, moved, "someday")

	verifyOut, verifyErr := tc.RunCampInDir(path, "triage", "verify")
	t.Logf("\n===== MIXED VERIFY =====\n%s", verifyOut)
	require.NoError(t, verifyErr, verifyOut)
	assert.Contains(t, verifyOut, "0 mismatched")

	report := readVerification(t, tc, runDir)
	assert.Equal(t, 4, report.Totals.Checked, "every approved row is accounted for")
	assert.Equal(t, 0, report.Totals.Unapplied)

	status, err := tc.RunCampInDir(path, "triage", "status")
	require.NoError(t, err, status)
	assert.Contains(t, status, "verified")
}

// TestAcceptance_StaleRowIsSkippedAndReported covers the other half of the
// mixed-fixture story: a workitem that disappears under an approved verdict.
//
// Camp catches it as a staleness precondition before the mover runs, skips the
// row, and stales its verdict — so the decision is withdrawn rather than
// silently executed against something that moved. The run may then verify,
// because a staled verdict is no longer a decision camp owes.
//
// The case where an approved verdict stays live and does not execute — the
// phase 006 blocker — is `TestVerifyCountsAnApprovedRowThatNeverApplied` in
// internal/triage, where the failure can be constructed exactly rather than
// provoked through the filesystem.
func TestAcceptance_StaleRowIsSkippedAndReported(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-acceptance-partial", 2, 1)

	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	require.Len(t, ids, 3)
	for _, id := range ids {
		disposition := "parked"
		if strings.HasPrefix(id, "intent-") {
			disposition = "someday"
		}
		judgeRow(t, tc, path, id, disposition)
		out, err := tc.RunCampInDir(path, "triage", "approve", id, "--json")
		require.NoError(t, err, out)
	}

	// Delete the intent out from under its approved verdict.
	tc.Shell(t, "cd "+path+" && rm -f .campaign/intents/inbox/intent-item-1.md")

	applyOut, _ := tc.RunCampInDir(path, "triage", "apply")
	t.Logf("\n===== APPLY WITH A VANISHED ROW =====\n%s", applyOut)
	assert.Contains(t, applyOut, "Applied 2 row(s)")
	assert.Contains(t, applyOut, "skipped intent-item-1",
		"the row is named, not quietly dropped")

	status, err := tc.RunCampInDir(path, "triage", "status")
	require.NoError(t, err, status)
	t.Logf("\n===== STATUS =====\n%s", status)
	assert.Contains(t, status, "stale",
		"the verdict is withdrawn rather than left looking live")
}

// TestAcceptance_NothingApprovedStillVerifiesHonestly keeps the empty case
// exactly as it was: a run that approved nothing has nothing to prove, and
// says so rather than being dragged into the unapplied accounting above.
func TestAcceptance_NothingApprovedStillVerifiesHonestly(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-acceptance-nothing", 2, 0)
	startTriageRun(t, tc, path)

	verifyOut, err := tc.RunCampInDir(path, "triage", "verify")
	require.NoError(t, err, verifyOut)
	t.Logf("\n===== VERIFY WITH NOTHING APPROVED =====\n%s", verifyOut)
	assert.Contains(t, verifyOut, "0 checked, 0 matched, 0 mismatched")
	assert.Contains(t, verifyOut, "Nothing has been applied yet, so there is nothing to prove.")
}
