//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// proposeRow records evidence and a proposal so a row is approvable.
func proposeRow(t *testing.T, tc *TestContainer, path, stableID, disposition string) {
	t.Helper()
	out, err := tc.RunCampInDir(path, "triage", "evidence", "set", stableID, "--no-evidence", "--json")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir(path, "triage", "propose", stableID,
		"--disposition", disposition, "--summary", "for the approve test", "--json")
	require.NoError(t, err, out)
}

// approvalOf returns the approval payload of `camp triage approve --json`.
func approvalOf(t *testing.T, output string) map[string]any {
	t.Helper()
	return decodeTriageJSON(t, output)["approval"].(map[string]any)
}

// recordedIDs lists the stable ids a call recorded.
func recordedIDs(t *testing.T, approval map[string]any) []string {
	t.Helper()
	raw, ok := approval["recorded"].([]any)
	require.True(t, ok, "recorded must always be an array")
	ids := make([]string, 0, len(raw))
	for _, entry := range raw {
		ids = append(ids, entry.(map[string]any)["stable_id"].(string))
	}
	return ids
}

// --- the terminal exclusion --------------------------------------------

// TestTriageApprove_BulkSelectorSkipsTerminalRows is the safety rule this
// sequence exists for: approving a batch is not meaningful consent to each
// irreversible action inside it.
func TestTriageApprove_BulkSelectorSkipsTerminalRows(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-approve-terminal", 4, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	require.Len(t, ids, 4)

	proposeRow(t, tc, path, ids[0], "parked")
	proposeRow(t, tc, path, ids[1], "next")
	proposeRow(t, tc, path, ids[2], "completed")   // terminal
	proposeRow(t, tc, path, ids[3], "consolidate") // terminal (split)

	output, err := tc.RunCampInDir(path, "triage", "approve", "--batch", "1", "--json")
	require.NoError(t, err, output)

	approval := approvalOf(t, output)
	assert.ElementsMatch(t, []string{ids[0], ids[1]}, recordedIDs(t, approval))
	skipped := approval["skipped_terminal"].([]any)
	assert.Len(t, skipped, 2, "both the dungeon move and the split are skipped")

	// The refusal has to name them, or the operator thinks the batch is done.
	text, err := tc.RunCampInDir(path, "triage", "approve", "--batch", "1")
	require.NoError(t, err, text)
	assert.Contains(t, text, "approve individually: camp triage approve")
	assert.Contains(t, text, ids[2])
	assert.Contains(t, text, ids[3])
}

// TestTriageApprove_NamingATerminalRowWorksAndEchoesTheCommand: the exclusion
// is about bulk selection, not about forbidding the action.
func TestTriageApprove_NamingATerminalRowWorksAndEchoesTheCommand(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-approve-named-terminal", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	proposeRow(t, tc, path, ids[0], "completed")

	output, err := tc.RunCampInDir(path, "triage", "approve", ids[0])
	require.NoError(t, err, output)

	assert.Contains(t, output, "Recorded 1 verdict")
	assert.Contains(t, output, "apply will run: camp workitem promote "+ids[0]+" --target completed",
		"approving a terminal action shows the real mutation, not just a label")
}

// --- selectors ---------------------------------------------------------

// TestTriageApprove_LaneSelector covers approving a whole lane.
func TestTriageApprove_LaneSelector(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-approve-lane", 3, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	for _, id := range ids {
		proposeRow(t, tc, path, id, "parked")
	}

	output, err := tc.RunCampInDir(path, "triage", "approve", "--lane", "park-for-later", "--json")
	require.NoError(t, err, output)

	assert.ElementsMatch(t, ids, recordedIDs(t, approvalOf(t, output)))
}

// TestTriageApprove_RejectsMoreThanOneSelectorForm keeps the intent explicit.
func TestTriageApprove_RejectsMoreThanOneSelectorForm(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-approve-twoforms", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	proposeRow(t, tc, path, ids[0], "parked")

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage approve "+ids[0]+" --lane park-for-later 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.NotContains(t, output, "EXIT:0")
	assert.Contains(t, output, "one selector")
}

// TestTriageApprove_ExitsTwoWhenNothingIsRecorded covers every reason nothing
// lands: an unknown lane, rows with no proposal, and an all-terminal bulk
// selector.
func TestTriageApprove_ExitsTwoWhenNothingIsRecorded(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-approve-exit2", 3, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	proposeRow(t, tc, path, ids[0], "completed") // terminal only

	// The two exit codes mean different things, and the distinction is the
	// point: 1 is "you typed something invalid", 2 is "your request was fine
	// but nothing could be recorded, and here is what to do first".
	tests := []struct {
		name string
		args string
		exit string
		want string
	}{
		{"no proposal on a real row", ids[1], "EXIT:2", "need a proposal first"},
		{"only terminal rows in the lane", "--lane close-as-delivered", "EXIT:2", "approve the terminal rows by name"},
		{"a lane that does not exist", "--lane nonexistent", "EXIT:1", "no lane named"},
	}

	for _, tc2 := range tests {
		t.Run(tc2.name, func(t *testing.T) {
			output, _, err := tc.ExecCommand("sh", "-c",
				"cd "+path+" && /camp triage approve "+tc2.args+" 2>&1; echo EXIT:$?")
			require.NoError(t, err)

			assert.Contains(t, output, tc2.exit, "output was: %s", output)
			assert.Contains(t, output, tc2.want)
		})
	}
}

// --- amend and reject --------------------------------------------------

// TestTriageApprove_AmendRevalidatesTheDisposition.
func TestTriageApprove_AmendRevalidatesTheDisposition(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-approve-amend", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	proposeRow(t, tc, path, ids[0], "parked")

	output, err := tc.RunCampInDir(path, "triage", "approve", ids[0], "--amend", "archived", "--json")
	require.NoError(t, err, output)
	recorded := approvalOf(t, output)["recorded"].([]any)[0].(map[string]any)
	assert.Equal(t, "amended", recorded["event"])
	assert.Equal(t, "archived", recorded["disposition"])
	assert.Equal(t, "dungeon/archived", recorded["canonical_action"])

	// An amendment outside the row's vocabulary is refused.
	proposeRow(t, tc, path, ids[1], "parked")
	bad, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage approve "+ids[1]+" --amend promote 2>&1; echo EXIT:$?")
	require.NoError(t, err)
	assert.NotContains(t, bad, "EXIT:0")
	assert.Contains(t, bad, "completed", "the refusal lists the row's real vocabulary")
}

// TestTriageApprove_RejectReturnsTheRowToTheQueue.
func TestTriageApprove_RejectReturnsTheRowToTheQueue(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-approve-reject", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	proposeRow(t, tc, path, ids[0], "parked")

	output, err := tc.RunCampInDir(path, "triage", "approve", ids[0],
		"--reject", "--note", "not this cycle", "--json")
	require.NoError(t, err, output)
	assert.Equal(t, "rejected",
		approvalOf(t, output)["recorded"].([]any)[0].(map[string]any)["event"])

	queueOut, err := tc.RunCampInDir(path, "triage", "queue", "--role", "synthesis", "--json")
	require.NoError(t, err, queueOut)
	items := queueItems(t, queueOut)
	var requeued bool
	for _, item := range items {
		if item["stable_id"] == ids[0] {
			requeued = true
		}
	}
	assert.True(t, requeued, "a rejected row needs a new proposal")
}

// --- idempotence and re-render -----------------------------------------

// TestTriageApprove_DoubleApproveIsReportedAsUnchanged: re-running a lane must
// not double-write the rest of it.
func TestTriageApprove_DoubleApproveIsReportedAsUnchanged(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-approve-idempotent", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	proposeRow(t, tc, path, ids[0], "parked")

	first, err := tc.RunCampInDir(path, "triage", "approve", ids[0], "--json")
	require.NoError(t, err, first)
	firstStream, err := tc.ReadFile(runDir + "/decisions.jsonl")
	require.NoError(t, err)

	second, err := tc.RunCampInDir(path, "triage", "approve", ids[0], "--json")
	require.NoError(t, err, second)
	recorded := approvalOf(t, second)["recorded"].([]any)[0].(map[string]any)
	assert.Equal(t, true, recorded["unchanged"])

	secondStream, err := tc.ReadFile(runDir + "/decisions.jsonl")
	require.NoError(t, err)
	assert.Equal(t, firstStream, secondStream, "no duplicate event was appended")
}

// TestTriageApprove_ReRendersTheDocuments: the documents must never lag the
// fold, so approve refreshes them rather than making the operator remember.
func TestTriageApprove_ReRendersTheDocuments(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-approve-rerender", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	proposeRow(t, tc, path, ids[0], "parked")

	output, err := tc.RunCampInDir(path, "triage", "approve", ids[0], "--json")
	require.NoError(t, err, output)
	assert.NotNil(t, decodeTriageJSON(t, output)["rendered"], "the render is reported")

	review, err := tc.ReadFile(runDir + "/TRIAGE_REVIEW.md")
	require.NoError(t, err, "approve rendered the review without being asked")
	assert.Contains(t, review, "## Approval record")
	assert.Contains(t, review, "| `"+ids[0]+"` | approved |")
}

// --- the trial's scenario ----------------------------------------------

// TestTriageApprove_PartialApprovalAcrossTwoSittings replays exactly what
// hand-editing broke in the 2026-08-03 field trial: approve a few rows, come
// back later, approve a few more, and the fold and the rendered document must
// both stay coherent with the rest still pending.
func TestTriageApprove_PartialApprovalAcrossTwoSittings(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-approve-partial", 6, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	require.Len(t, ids, 6)
	for _, id := range ids {
		proposeRow(t, tc, path, id, "parked")
	}

	// First sitting: two rows.
	first, err := tc.RunCampInDir(path, "triage", "approve", ids[0], ids[1], "--json")
	require.NoError(t, err, first)
	assert.Len(t, recordedIDs(t, approvalOf(t, first)), 2)

	status, err := tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, status)
	counts := decodeTriageJSON(t, status)["run"].(map[string]any)["counts"].(map[string]any)
	assert.Equal(t, float64(2), counts["approved"])
	assert.Equal(t, float64(4), counts["proposed"], "the rest are untouched, not implied")

	// Second sitting: one new row plus one already approved.
	second, err := tc.RunCampInDir(path, "triage", "approve", ids[2], ids[0], "--json")
	require.NoError(t, err, second)
	recorded := approvalOf(t, second)["recorded"].([]any)
	unchanged := 0
	for _, entry := range recorded {
		if entry.(map[string]any)["unchanged"] == true {
			unchanged++
		}
	}
	assert.Equal(t, 1, unchanged, "the row from the first sitting is unchanged, not re-recorded")

	status, err = tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, status)
	counts = decodeTriageJSON(t, status)["run"].(map[string]any)["counts"].(map[string]any)
	assert.Equal(t, float64(3), counts["approved"])
	assert.Equal(t, float64(3), counts["proposed"])

	// The document agrees with the fold, and still shows the pending rows.
	review, err := tc.ReadFile(runDir + "/TRIAGE_REVIEW.md")
	require.NoError(t, err)
	for _, id := range ids[:3] {
		assert.Contains(t, review, "| `"+id+"` | approved |")
	}
	assert.Contains(t, review, "| **Total** | **6** |")

	// Every row still appears exactly once across the lane tables. The count
	// is scoped to that section: the approval record below it legitimately
	// lists approved rows again, and counting the whole document would
	// conflate the two.
	decisions := sectionOf(review, "## Proposed portfolio decisions", "## Resulting portfolio shape")
	for _, id := range ids {
		assert.Equal(t, 1, strings.Count(decisions, "| `"+id+"` |"),
			"row %s appears exactly once in the lane tables", id)
	}
}

// sectionOf returns the document text between two headings.
func sectionOf(doc, start, end string) string {
	from := strings.Index(doc, start)
	if from < 0 {
		return ""
	}
	rest := doc[from:]
	to := strings.Index(rest, end)
	if to < 0 {
		return rest
	}
	return rest[:to]
}

// TestTriageApprove_JSONIsParseableByARealConsumer validates the contract with
// jq rather than only with our own unmarshaler.
func TestTriageApprove_JSONIsParseableByARealConsumer(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-approve-jq", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	proposeRow(t, tc, path, ids[0], "parked")

	out := tc.Shell(t, "cd "+path+" && /camp triage approve "+ids[0]+
		" --json | jq -r '.schema_version, (.approval.recorded | length), .approval.recorded[0].event'")

	fields := strings.Fields(strings.TrimSpace(out))
	require.Len(t, fields, 3, "jq should read three fields: %s", out)
	assert.Equal(t, "triage-approve/v1alpha1", fields[0])
	assert.Equal(t, "1", fields[1])
	assert.Equal(t, "approved", fields[2])
}
