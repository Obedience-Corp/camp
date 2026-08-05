//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTriageVerify_CleanAfterApply is the Done When's first half: apply a
// fixture, then verify it clean.
func TestTriageVerify_CleanAfterApply(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-verify-clean", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	approveAll(t, tc, path, manifestRowIDs(t, tc, runDir), "parked")

	out, err := tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, out)

	output, err := tc.RunCampInDir(path, "triage", "verify")
	require.NoError(t, err, output)
	assert.Contains(t, output, "2 checked, 2 matched, 0 mismatched")
	assert.Contains(t, output, "where its approved verdict said it would be")

	// Both artifacts exist and the data drives the document.
	raw, err := tc.ReadFile(runDir + "/verification.json")
	require.NoError(t, err)
	var report struct {
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
	require.NoError(t, json.Unmarshal([]byte(raw), &report))
	assert.Equal(t, 2, report.Totals.Checked)
	assert.Equal(t, 2, report.Totals.Matched)
	for _, row := range report.Rows {
		assert.Equal(t, "match", row.Result)
		assert.Equal(t, "parked", row.ExpectedStage)
	}

	doc, err := tc.ReadFile(runDir + "/VERIFICATION.md")
	require.NoError(t, err)
	assert.Contains(t, doc, "Do not edit")
	assert.Contains(t, doc, "design-item-1")

	// A clean verification is what closes the run.
	status, err := tc.RunCampInDir(path, "triage", "status")
	require.NoError(t, err, status)
	assert.Contains(t, status, "verified")
}

// TestTriageVerify_TamperedExitsOneWithNamedRows is the Done When's second
// half: move a workitem back, and verify must fail and say which.
func TestTriageVerify_TamperedExitsOneWithNamedRows(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-verify-tampered", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	approveAll(t, tc, path, manifestRowIDs(t, tc, runDir), "parked")

	out, err := tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, out)

	// Someone un-parks a row after the fact.
	out, err = tc.RunCampInDir(path, "workitem", "stage", "design-item-1", "active")
	require.NoError(t, err, out)

	output, err := tc.RunCampInDir(path, "triage", "verify")
	require.Error(t, err, "an unexplained mismatch must exit non-zero")
	assert.Contains(t, output, "mismatch design-item-1")
	assert.Contains(t, output, "parked")
	assert.Contains(t, output, "active")
	assert.Contains(t, output, "1 matched, 1 mismatched")

	raw, err := tc.ReadFile(runDir + "/verification.json")
	require.NoError(t, err)
	// The file is written indented, so match the rendered form.
	assert.Contains(t, raw, `"result": "mismatch"`)
	assert.Contains(t, raw, "design-item-1")
}

// TestTriageVerify_RetiredWorkitemVerifiesByBeingGone covers the terminal
// path against a real dungeon move.
func TestTriageVerify_RetiredWorkitemVerifiesByBeingGone(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-verify-retired", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	approveAll(t, tc, path, manifestRowIDs(t, tc, runDir), "completed")

	out, err := tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, out)

	output, err := tc.RunCampInDir(path, "triage", "verify")
	require.NoError(t, err, output)
	assert.Contains(t, output, "0 mismatched")

	raw, err := tc.ReadFile(runDir + "/verification.json")
	require.NoError(t, err)
	// Matches both the hidden .dungeon spelling and the older dungeon/ one;
	// which a campaign uses depends on whether it has been migrated.
	assert.Contains(t, raw, "dungeon/completed",
		"the expected path is where the workitem actually landed")
}

// TestTriageVerify_JSONIsParseableByARealConsumer pins the contract.
func TestTriageVerify_JSONIsParseableByARealConsumer(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-verify-json", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	approveAll(t, tc, path, manifestRowIDs(t, tc, runDir), "parked")
	out, err := tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, out)

	output, err := tc.RunCampInDir(path, "triage", "verify", "--json")
	require.NoError(t, err, output)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &payload))
	for _, key := range []string{"schema_version", "report", "report_path", "document_path", "clean"} {
		assert.Contains(t, payload, key)
	}
	assert.Equal(t, "triage-verify/v1alpha1", payload["schema_version"])
	assert.Equal(t, true, payload["clean"])

	report := payload["report"].(map[string]any)
	assert.IsType(t, []any{}, report["rows"])
	assert.Contains(t, report, "totals")
}
