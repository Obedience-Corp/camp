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

// startTriageRun opens a run and returns its id and directory.
func startTriageRun(t *testing.T, tc *TestContainer, path string) (string, string) {
	t.Helper()
	out, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, out)
	runID := decodeTriageJSON(t, out)["run_id"].(string)
	return runID, path + "/.campaign/triage/runs/" + runID
}

// queueItems reads the queue payload's items.
func queueItems(t *testing.T, output string) []map[string]any {
	t.Helper()
	queue := decodeTriageJSON(t, output)["queue"].(map[string]any)
	raw, ok := queue["items"].([]any)
	require.True(t, ok, "queue must always carry an items array: %s", output)
	items := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		items = append(items, entry.(map[string]any))
	}
	return items
}

// --- queue -------------------------------------------------------------

// TestTriageQueue_ListsRowsAwaitingEvidence is the driver contract's read
// half: everything needed to start work, in one call.
func TestTriageQueue_ListsRowsAwaitingEvidence(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-queue-basic", 4, 2)
	startTriageRun(t, tc, path)

	output, err := tc.RunCampInDir(path, "triage", "queue", "--json")
	require.NoError(t, err, output)

	payload := decodeTriageJSON(t, output)
	assert.Equal(t, "triage-queue/v1alpha1", payload["schema_version"])

	queue := payload["queue"].(map[string]any)
	assert.Equal(t, "triage/v1alpha1", queue["evidence_schema_version"],
		"a driver must be told which record format to produce")

	routing, ok := queue["routing"].(map[string]any)
	require.True(t, ok, "the advisory routing block is passed through verbatim")
	assert.Equal(t, "cheap", routing["evidence_tier"])
	assert.Equal(t, float64(4), routing["max_concurrent"])

	items := queueItems(t, output)
	require.Len(t, items, 6)
	for _, item := range items {
		assert.Equal(t, "evidence", item["role"])
		assert.NotEmpty(t, item["stable_id"])
		assert.NotEmpty(t, item["relative_path"])
		assert.GreaterOrEqual(t, item["batch"], float64(1))
		policy := item["policy"].(map[string]any)
		assert.NotEmpty(t, policy["evidence"])
		assert.NotEmpty(t, policy["routing_tier"])
	}

	counts := queue["counts"].(map[string]any)
	assert.Equal(t, float64(6), counts["evidence"])
	assert.Equal(t, float64(0), counts["done"])
}

// TestTriageQueue_RejectsAnUnknownRole: a mistyped role must not quietly
// return everything, which would look like the filter worked.
func TestTriageQueue_RejectsAnUnknownRole(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-queue-badrole", 2, 0)
	startTriageRun(t, tc, path)

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage queue --role reviewer 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.NotContains(t, output, "EXIT:0", "a bad role must fail: %s", output)
	assert.Contains(t, output, "evidence")
	assert.Contains(t, output, "synthesis")
}

// --- evidence template -------------------------------------------------

// TestTriageEvidenceTemplate_PreFillsFactsNotJudgment is the zero-judgment
// path made real: camp fills in what it measured and leaves what must be
// decided empty.
func TestTriageEvidenceTemplate_PreFillsFactsNotJudgment(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-template", 3, 0)
	_, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)

	output, err := tc.RunCampInDir(path, "triage", "evidence", "template", stableID)
	require.NoError(t, err, output)

	var record map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &record),
		"the template must be valid JSON a driver can edit: %s", output)

	assert.Equal(t, "triage/v1alpha1", record["schema_version"])
	assert.Equal(t, stableID, record["stable_id"])
	assert.Equal(t, "", record["original_goal"], "judgment stays empty")
	assert.Equal(t, "", record["confidence"])

	signals := record["signals"].(map[string]any)
	assert.Equal(t, "design", signals["type"])
	assert.NotEmpty(t, signals["relative_path"])
	assert.NotEmpty(t, signals["age_days"], "camp knows how old the row is")

	producedBy := record["produced_by"].(map[string]any)
	assert.Equal(t, "deterministic", producedBy["role"],
		"a template is camp's own output, not a reading")

	anchors := record["anchors"].([]any)
	require.NotEmpty(t, anchors)
	var kinds []string
	for _, entry := range anchors {
		kinds = append(kinds, entry.(map[string]any)["kind"].(string))
	}
	assert.Contains(t, kinds, "path")
	assert.NotContains(t, kinds, "pr", "camp never fabricates a PR anchor")

	for _, entry := range anchors {
		anchor := entry.(map[string]any)
		if anchor["kind"] == "path" {
			assert.True(t, strings.HasPrefix(anchor["hash"].(string), "sha256:"),
				"the path anchor carries a real measurement")
		}
	}
}

// TestTriageEvidenceTemplate_RoundTripsThroughSet closes the loop the command
// promises: template out, fill in, submit back.
func TestTriageEvidenceTemplate_RoundTripsThroughSet(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-template-roundtrip", 2, 0)
	runID, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)

	// Template out, fill the judgment fields in with jq, submit back.
	script := "cd " + path + " && /camp triage evidence template " + stableID + " > /tmp/rec.json && " +
		`jq '.original_goal = "Ship the thing" | .confidence = "medium" | ` +
		`.delivered = ["the core"] | .missing = ["the docs"] | ` +
		`.produced_by.role = "human" | .produced_by.runtime = "human"' /tmp/rec.json > /tmp/filled.json && ` +
		"/camp triage evidence set " + stableID + " --file /tmp/filled.json --json"
	output := tc.Shell(t, script)

	payload := decodeTriageJSON(t, output)
	assert.Equal(t, stableID, payload["stable_id"])
	assert.Equal(t, true, payload["written"])

	stored, err := tc.ReadFile(runDir + "/evidence/" + evidenceFileName(stableID))
	require.NoError(t, err)
	assert.Contains(t, stored, "Ship the thing")
	assert.Contains(t, stored, `"signals"`, "camp's measured facts survive the round trip")
	assert.Contains(t, stored, "sha256:")

	// And the row has moved on to needing a proposal, not another reading.
	queueOut, err := tc.RunCampInDir(path, "triage", "queue", "--role", "synthesis", "--json")
	require.NoError(t, err, queueOut)
	items := queueItems(t, queueOut)
	require.Len(t, items, 1)
	assert.Equal(t, stableID, items[0]["stable_id"])
	_ = runID
}

// evidenceFileName mirrors the store's filename rule for clean slugs, which is
// all these fixtures use.
func evidenceFileName(stableID string) string { return stableID + ".json" }

// --- evidence set ------------------------------------------------------

// TestTriageEvidenceSet_RejectsAnInvalidRecordNamingEveryField is the floor
// that makes any driver safe: one submission, one complete list of what to fix.
func TestTriageEvidenceSet_RejectsAnInvalidRecordNamingEveryField(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-evidence-invalid", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)

	record := `{"schema_version":"triage/v1alpha1","stable_id":"` + stableID + `",` +
		`"original_goal":"","confidence":"certain",` +
		`"produced_by":{"role":"oracle","runtime":"","at":"2026-08-10T14:00:00Z"}}`
	tc.Shell(t, "cd "+path+" && cat > /tmp/bad.json <<'EOF'\n"+record+"\nEOF")

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage evidence set "+stableID+" --file /tmp/bad.json 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.NotContains(t, output, "EXIT:0", "an invalid record must be refused: %s", output)
	for _, field := range []string{"original_goal", "confidence", "produced_by.role", "produced_by.runtime"} {
		assert.Contains(t, output, field, "every violated field must be named")
	}
	assert.Contains(t, output, "high, medium, low", "with the values that would work")

	exists, err := tc.CheckFileExists(runDir + "/evidence/" + evidenceFileName(stableID))
	require.NoError(t, err)
	assert.False(t, exists, "a rejected record must not reach the run")
}

// TestTriageEvidenceSet_RejectsAnUnknownStableID names the id rather than
// writing a record for a row that is not in the run.
func TestTriageEvidenceSet_RejectsAnUnknownStableID(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-evidence-unknownid", 2, 0)
	startTriageRun(t, tc, path)

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage evidence set no-such-row --no-evidence 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.NotContains(t, output, "EXIT:0")
	assert.Contains(t, output, "no-such-row")
}

// TestTriageEvidenceSet_IsIdempotentByContent: a driver retrying a batch must
// not churn the run, but a genuine revision must land.
func TestTriageEvidenceSet_IsIdempotentByContent(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-evidence-idempotent", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)

	record := `{"schema_version":"triage/v1alpha1","stable_id":"` + stableID + `",` +
		`"original_goal":"A goal","delivered":["x"],"missing":[],"stale_assumptions":[],` +
		`"related":[],"open_decisions":[],"confidence":"high","anchors":[],` +
		`"produced_by":{"role":"evidence","runtime":"fixture","at":"2026-08-10T14:00:00Z"}}`
	tc.Shell(t, "cd "+path+" && cat > /tmp/rec.json <<'EOF'\n"+record+"\nEOF")

	first, err := tc.RunCampInDir(path, "triage", "evidence", "set", stableID, "--file", "/tmp/rec.json", "--json")
	require.NoError(t, err, first)
	assert.Equal(t, true, decodeTriageJSON(t, first)["written"])

	second, err := tc.RunCampInDir(path, "triage", "evidence", "set", stableID, "--file", "/tmp/rec.json", "--json")
	require.NoError(t, err, second)
	assert.Equal(t, false, decodeTriageJSON(t, second)["written"],
		"an identical resubmission changes nothing")

	tc.Shell(t, "cd "+path+" && jq '.confidence = \"low\"' /tmp/rec.json > /tmp/rec2.json")
	third, err := tc.RunCampInDir(path, "triage", "evidence", "set", stableID, "--file", "/tmp/rec2.json", "--json")
	require.NoError(t, err, third)
	assert.Equal(t, true, decodeTriageJSON(t, third)["written"], "a real revision lands")
}

// TestTriageEvidenceSet_RejectsAMismatchedRecord: a copy-pasted record must
// not silently overwrite the wrong row.
func TestTriageEvidenceSet_RejectsAMismatchedRecord(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-evidence-mismatch", 3, 0)
	_, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)

	record := `{"schema_version":"triage/v1alpha1","stable_id":"some-other-row",` +
		`"original_goal":"A goal","delivered":[],"missing":[],"stale_assumptions":[],` +
		`"related":[],"open_decisions":[],"confidence":"high","anchors":[],` +
		`"produced_by":{"role":"evidence","runtime":"fixture","at":"2026-08-10T14:00:00Z"}}`
	tc.Shell(t, "cd "+path+" && cat > /tmp/mismatch.json <<'EOF'\n"+record+"\nEOF")

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage evidence set "+stableID+" --file /tmp/mismatch.json 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.NotContains(t, output, "EXIT:0")
	assert.Contains(t, output, "some-other-row")
	assert.Contains(t, output, stableID)
}

// TestTriageEvidenceSet_NoEvidenceMarker records a row judged without a
// gathered record - a real answer, not a missing one.
func TestTriageEvidenceSet_NoEvidenceMarker(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-evidence-none", 3, 0)
	_, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)

	output, err := tc.RunCampInDir(path, "triage", "evidence", "set", stableID, "--no-evidence", "--json")
	require.NoError(t, err, output)

	payload := decodeTriageJSON(t, output)
	assert.Equal(t, true, payload["no_evidence"])
	assert.Equal(t, true, payload["written"])

	stored, err := tc.ReadFile(runDir + "/evidence/" + evidenceFileName(stableID))
	require.NoError(t, err)
	assert.Contains(t, stored, `"no_evidence": true`)
	assert.Contains(t, stored, `"role": "human"`)

	// It satisfies the same requirement a full record does: the row moves on
	// to needing a proposal.
	queueOut, err := tc.RunCampInDir(path, "triage", "queue", "--role", "synthesis", "--json")
	require.NoError(t, err, queueOut)
	items := queueItems(t, queueOut)
	require.Len(t, items, 1)
	assert.Equal(t, stableID, items[0]["stable_id"])
}

// TestTriageEvidenceSet_RefusesFileAndNoEvidenceTogether: a no-evidence row
// has no record to read, so the combination is a contradiction.
func TestTriageEvidenceSet_RefusesFileAndNoEvidenceTogether(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-evidence-conflict", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	stableID := firstManifestStableID(t, tc, runDir)

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage evidence set "+stableID+
			" --file /tmp/whatever.json --no-evidence 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.NotContains(t, output, "EXIT:0")
	assert.Contains(t, output, "no-evidence")
}

// TestTriageEvidence_JSONIsParseableByARealConsumer validates the contract
// with jq rather than only with our own unmarshaler.
func TestTriageEvidence_JSONIsParseableByARealConsumer(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-judgment-jq", 3, 1)
	startTriageRun(t, tc, path)

	out := tc.Shell(t, "cd "+path+
		" && /camp triage queue --json | jq -r '.queue.counts.evidence, (.queue.items | length), .queue.evidence_schema_version'")

	fields := strings.Fields(strings.TrimSpace(out))
	require.Len(t, fields, 3, "jq should read three fields: %s", out)
	assert.Equal(t, "4", fields[0])
	assert.Equal(t, "4", fields[1])
	assert.Equal(t, "triage/v1alpha1", fields[2])
}
