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

// recordEvidenceFor stores a minimal valid record so a row is ready to be
// proposed on.
func recordEvidenceFor(t *testing.T, tc *TestContainer, path, stableID string) {
	t.Helper()
	out, err := tc.RunCampInDir(path, "triage", "evidence", "set", stableID, "--no-evidence", "--json")
	require.NoError(t, err, out)
}

// decisionLines parses a run's verdict stream in append order.
func decisionLines(t *testing.T, tc *TestContainer, runDir string) []map[string]any {
	t.Helper()
	raw, err := tc.ReadFile(runDir + "/decisions.jsonl")
	require.NoError(t, err)

	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &event), "line: %s", line)
		events = append(events, event)
	}
	return events
}

// --- propose -----------------------------------------------------------

// TestTriagePropose_RecordsTheActionNotJustTheLabel is what makes apply
// mechanical: the label is the campaign's word, the action is camp's.
func TestTriagePropose_RecordsTheActionNotJustTheLabel(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-propose-basic", 3, 0)
	_, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)
	recordEvidenceFor(t, tc, path, stableID)

	output, err := tc.RunCampInDir(path, "triage", "propose", stableID,
		"--disposition", "completed", "--summary", "shipped in PR 239", "--json")
	require.NoError(t, err, output)

	payload := decodeTriageJSON(t, output)
	assert.Equal(t, "triage-propose/v1alpha1", payload["schema_version"])

	proposal := payload["proposal"].(map[string]any)
	assert.Equal(t, stableID, proposal["stable_id"])
	assert.Equal(t, "completed", proposal["disposition"])
	assert.Equal(t, "dungeon/completed", proposal["canonical_action"])
	assert.Equal(t, true, proposal["requires_approval"],
		"a terminal action is never settled by a proposal alone")
	assert.Contains(t, proposal["rationale_ref"], "rationales/")

	events := decisionLines(t, tc, runDir)
	require.Len(t, events, 1)
	assert.Equal(t, "proposed", events[0]["event"])
	assert.Equal(t, "dungeon/completed", events[0]["canonical_action"])
	assert.NotEmpty(t, events[0]["actor"], "verdicts are attributed")
	assert.NotEmpty(t, events[0]["rationale_ref"])
}

// TestTriagePropose_RejectsAnUnknownDisposition lists the vocabulary that
// would have worked rather than guessing.
func TestTriagePropose_RejectsAnUnknownDisposition(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-propose-baddisp", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)
	recordEvidenceFor(t, tc, path, stableID)

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage propose "+stableID+
			" --disposition shelve --summary x 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.NotContains(t, output, "EXIT:0", "an unknown label must fail: %s", output)
	assert.Contains(t, output, "completed")
	assert.Contains(t, output, "consolidate")

	exists, err := tc.CheckFileExists(runDir + "/decisions.jsonl")
	require.NoError(t, err)
	assert.False(t, exists, "a refused proposal records nothing")
}

// TestTriagePropose_SupersedeChainIsVisibleInTheStream is the requirement that
// nothing is overwritten.
func TestTriagePropose_SupersedeChainIsVisibleInTheStream(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-propose-supersede", 3, 0)
	_, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)
	recordEvidenceFor(t, tc, path, stableID)

	first, err := tc.RunCampInDir(path, "triage", "propose", stableID,
		"--disposition", "completed", "--summary", "looks shipped", "--json")
	require.NoError(t, err, first)

	second, err := tc.RunCampInDir(path, "triage", "propose", stableID,
		"--disposition", "parked", "--summary", "actually, revisit later", "--json")
	require.NoError(t, err, second)
	assert.Equal(t, "completed",
		decodeTriageJSON(t, second)["proposal"].(map[string]any)["superseded"])

	events := decisionLines(t, tc, runDir)
	require.Len(t, events, 3, "proposed, superseded, proposed")
	assert.Equal(t, "proposed", events[0]["event"])
	assert.Equal(t, "completed", events[0]["disposition"])
	assert.Equal(t, "superseded", events[1]["event"])
	assert.Equal(t, "completed", events[1]["disposition"],
		"the retired proposal is named, not blanked")
	assert.Equal(t, "proposed", events[2]["event"])
	assert.Equal(t, "parked", events[2]["disposition"])

	// Status folds the stream to the newest live proposal.
	statusOut, err := tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, statusOut)
	counts := decodeTriageJSON(t, statusOut)["run"].(map[string]any)["counts"].(map[string]any)
	assert.Equal(t, float64(1), counts["proposed"])
}

// TestTriagePropose_RationaleFromAFile covers the fuller rationale shape.
func TestTriagePropose_RationaleFromAFile(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-propose-file", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)
	recordEvidenceFor(t, tc, path, stableID)

	rationale := `{"schema_version":"triage/v1alpha1","summary":"delivered; nothing open",` +
		`"anchors_used":["pr:obey#239"],"confidence":"high"}`
	tc.Shell(t, "cd "+path+" && cat > /tmp/rationale.json <<'EOF'\n"+rationale+"\nEOF")

	output, err := tc.RunCampInDir(path, "triage", "propose", stableID,
		"--disposition", "parked", "--file", "/tmp/rationale.json", "--json")
	require.NoError(t, err, output)

	ref := decodeTriageJSON(t, output)["proposal"].(map[string]any)["rationale_ref"].(string)
	stored, err := tc.ReadFile(runDir + "/" + ref)
	require.NoError(t, err)
	assert.Contains(t, stored, "delivered; nothing open")
	assert.Contains(t, stored, "pr:obey#239")
}

// TestTriagePropose_RejectsAnInvalidRationaleFile names every violated field.
func TestTriagePropose_RejectsAnInvalidRationaleFile(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-propose-badfile", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)
	recordEvidenceFor(t, tc, path, stableID)

	bad := `{"schema_version":"triage/v1alpha1","summary":"","confidence":"certain"}`
	tc.Shell(t, "cd "+path+" && cat > /tmp/bad-rationale.json <<'EOF'\n"+bad+"\nEOF")

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage propose "+stableID+
			" --disposition parked --file /tmp/bad-rationale.json 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.NotContains(t, output, "EXIT:0")
	assert.Contains(t, output, "summary")
	assert.Contains(t, output, "confidence")
	assert.Contains(t, output, "high, medium, low")
}

// TestTriagePropose_IntentVocabularyDiffersFromDesign proves the per-type
// vocabularies are real and not one shared list.
func TestTriagePropose_IntentVocabularyDiffersFromDesign(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-propose-typevocab", 1, 1)
	_, runDir := startTriageRun(t, tc, path)

	raw, err := tc.ReadFile(runDir + "/manifest.json")
	require.NoError(t, err)
	var manifest struct {
		Rows []struct {
			StableID string `json:"stable_id"`
			Type     string `json:"type"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &manifest))

	var intentID, designID string
	for _, row := range manifest.Rows {
		switch row.Type {
		case "intent":
			intentID = row.StableID
		case "design":
			designID = row.StableID
		}
	}
	require.NotEmpty(t, intentID)
	require.NotEmpty(t, designID)
	recordEvidenceFor(t, tc, path, intentID)
	recordEvidenceFor(t, tc, path, designID)

	// "ready" is an intent label and not a design one.
	out, err := tc.RunCampInDir(path, "triage", "propose", intentID,
		"--disposition", "ready", "--summary", "worth doing", "--json")
	require.NoError(t, err, out)
	assert.Equal(t, "rail/ready",
		decodeTriageJSON(t, out)["proposal"].(map[string]any)["canonical_action"])

	refused, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage propose "+designID+
			" --disposition ready --summary x 2>&1; echo EXIT:$?")
	require.NoError(t, err)
	assert.NotContains(t, refused, "EXIT:0",
		"a design has no ready label: %s", refused)
}

// --- the judging -> reviewing gate --------------------------------------

// TestTriageReviewGate_BlocksUntilEveryRowIsJudged is the phase discipline the
// sequence exists to enforce: rows nobody looked at must not reach a reviewer
// as blanks.
func TestTriageReviewGate_BlocksUntilEveryRowIsJudged(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-review-gate", 3, 0)
	_, runDir := startTriageRun(t, tc, path)

	raw, err := tc.ReadFile(runDir + "/manifest.json")
	require.NoError(t, err)
	var manifest struct {
		Rows []struct {
			StableID string `json:"stable_id"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &manifest))
	require.Len(t, manifest.Rows, 3)

	// Judge two of the three.
	for _, row := range manifest.Rows[:2] {
		recordEvidenceFor(t, tc, path, row.StableID)
		out, proposeErr := tc.RunCampInDir(path, "triage", "propose", row.StableID,
			"--disposition", "parked", "--summary", "later", "--json")
		require.NoError(t, proposeErr, out)
	}

	// The queue still shows the third, which is the gap the gate will name.
	queueOut, err := tc.RunCampInDir(path, "triage", "queue", "--json")
	require.NoError(t, err, queueOut)
	items := queueItems(t, queueOut)
	require.Len(t, items, 1)
	assert.Equal(t, manifest.Rows[2].StableID, items[0]["stable_id"])
	assert.Equal(t, "evidence", items[0]["role"])

	// Judging the last one empties the queue.
	last := manifest.Rows[2].StableID
	recordEvidenceFor(t, tc, path, last)
	out, err := tc.RunCampInDir(path, "triage", "propose", last,
		"--disposition", "parked", "--summary", "later", "--json")
	require.NoError(t, err, out)

	queueOut, err = tc.RunCampInDir(path, "triage", "queue", "--json")
	require.NoError(t, err, queueOut)
	assert.Empty(t, queueItems(t, queueOut))

	queue := decodeTriageJSON(t, queueOut)["queue"].(map[string]any)
	counts := queue["counts"].(map[string]any)
	assert.Equal(t, float64(3), counts["done"])
}

// TestTriageJudgment_FullDriverLoop walks the contract doc 08 defines end to
// end through the real binary: read the queue, submit evidence, propose, and
// watch the queue empty.
func TestTriageJudgment_FullDriverLoop(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-driver-loop", 2, 1)
	startTriageRun(t, tc, path)

	queueOut, err := tc.RunCampInDir(path, "triage", "queue", "--json")
	require.NoError(t, err, queueOut)
	items := queueItems(t, queueOut)
	require.Len(t, items, 3)

	for _, item := range items {
		id := item["stable_id"].(string)
		// The label has to come from the row's own type vocabulary: an intent
		// can be promoted with "ready", a design cannot. That difference is
		// the per-type policy doing its job.
		disposition := "parked"
		if item["type"].(string) == "intent" {
			disposition = "ready"
		}

		// Template out, fill in, submit back.
		script := "cd " + path + " && /camp triage evidence template " + id + " > /tmp/t.json && " +
			`jq '.original_goal = "goal" | .confidence = "medium" | ` +
			`.produced_by.role = "evidence" | .produced_by.runtime = "loop"' /tmp/t.json > /tmp/f.json && ` +
			"/camp triage evidence set " + id + " --file /tmp/f.json --json"
		out := tc.Shell(t, script)
		assert.Equal(t, true, decodeTriageJSON(t, out)["written"])

		out, err = tc.RunCampInDir(path, "triage", "propose", id,
			"--disposition", disposition, "--summary", "revisit next cycle", "--json")
		require.NoError(t, err, out)
	}

	queueOut, err = tc.RunCampInDir(path, "triage", "queue", "--json")
	require.NoError(t, err, queueOut)
	assert.Empty(t, queueItems(t, queueOut), "the loop terminates when the queue empties")

	statusOut, err := tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, statusOut)
	counts := decodeTriageJSON(t, statusOut)["run"].(map[string]any)["counts"].(map[string]any)
	assert.Equal(t, float64(3), counts["proposed"])
	assert.Equal(t, float64(0), counts["pending-evidence"])
}
