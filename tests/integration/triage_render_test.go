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

// judgeRow records evidence and a proposal so a row reaches a lane.
func judgeRow(t *testing.T, tc *TestContainer, path, stableID, disposition string) {
	t.Helper()
	out, err := tc.RunCampInDir(path, "triage", "evidence", "set", stableID, "--no-evidence", "--json")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir(path, "triage", "propose", stableID,
		"--disposition", disposition, "--summary", "recorded for the render test", "--json")
	require.NoError(t, err, out)
}

// manifestRowIDs returns a run's stable ids in manifest order.
func manifestRowIDs(t *testing.T, tc *TestContainer, runDir string) []string {
	t.Helper()
	raw, err := tc.ReadFile(runDir + "/manifest.json")
	require.NoError(t, err)
	var manifest struct {
		Rows []struct {
			StableID string `json:"stable_id"`
			Type     string `json:"type"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &manifest))
	ids := make([]string, 0, len(manifest.Rows))
	for _, row := range manifest.Rows {
		ids = append(ids, row.StableID)
	}
	return ids
}

// --- review ------------------------------------------------------------

// TestTriageReview_WritesBothDocuments is the sequence's core deliverable.
func TestTriageReview_WritesBothDocuments(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-render-basic", 4, 1)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	judgeRow(t, tc, path, ids[0], "completed")
	judgeRow(t, tc, path, ids[1], "parked")

	output, err := tc.RunCampInDir(path, "triage", "review", "--render-only", "--json")
	require.NoError(t, err, output)

	payload := decodeTriageJSON(t, output)
	assert.Equal(t, "triage-review/v1alpha1", payload["schema_version"])
	render := payload["render"].(map[string]any)
	assert.Equal(t, float64(5), render["rows"])
	assert.Contains(t, render["review_path"], "TRIAGE_REVIEW.md")
	assert.Contains(t, render["priorities_path"], "PRIORITIES.md")

	review, err := tc.ReadFile(runDir + "/TRIAGE_REVIEW.md")
	require.NoError(t, err)
	priorities, err := tc.ReadFile(runDir + "/PRIORITIES.md")
	require.NoError(t, err)

	// The D3 banner has to be where a user would start editing.
	for _, doc := range []string{review, priorities} {
		assert.Contains(t, doc, "Do not edit")
		assert.Contains(t, doc, "camp triage approve")
	}
	assert.Contains(t, review, "## Decision requested")
	assert.Contains(t, review, "## Recommended priority order")
	assert.Contains(t, review, "## Proposed portfolio decisions")
	assert.Contains(t, review, "## Resulting portfolio shape")
	assert.Contains(t, review, "## How this result was produced")
	assert.Contains(t, review, "## Approval record")
	assert.Contains(t, review, "Close as delivered")
	assert.Contains(t, review, "dungeon/completed")
}

// TestTriageReview_EveryRowAppearsExactlyOnce: a review that omitted a row
// could be approved without the operator seeing what it left out.
func TestTriageReview_EveryRowAppearsExactlyOnce(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-render-complete", 5, 2)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	require.Len(t, ids, 7)
	judgeRow(t, tc, path, ids[0], "completed")
	judgeRow(t, tc, path, ids[1], "parked")

	out, err := tc.RunCampInDir(path, "triage", "review", "--render-only")
	require.NoError(t, err, out)
	review, err := tc.ReadFile(runDir + "/TRIAGE_REVIEW.md")
	require.NoError(t, err)

	for _, id := range ids {
		assert.Equal(t, 1, strings.Count(review, "| `"+id+"` |"),
			"row %s must appear exactly once across the lane tables", id)
	}
	assert.Contains(t, review, "| **Total** | **7** |")
}

// TestTriageReview_IsByteStable: re-rendering unchanged data must be a no-op
// diff, or every review churns its own file.
func TestTriageReview_IsByteStable(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-render-stable", 3, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	judgeRow(t, tc, path, ids[0], "parked")

	out, err := tc.RunCampInDir(path, "triage", "review", "--render-only")
	require.NoError(t, err, out)
	first, err := tc.ReadFile(runDir + "/TRIAGE_REVIEW.md")
	require.NoError(t, err)
	firstPriorities, err := tc.ReadFile(runDir + "/PRIORITIES.md")
	require.NoError(t, err)

	out, err = tc.RunCampInDir(path, "triage", "review", "--render-only")
	require.NoError(t, err, out)
	second, err := tc.ReadFile(runDir + "/TRIAGE_REVIEW.md")
	require.NoError(t, err)
	secondPriorities, err := tc.ReadFile(runDir + "/PRIORITIES.md")
	require.NoError(t, err)

	assert.Equal(t, first, second, "the review must re-render byte-identically")
	assert.Equal(t, firstPriorities, secondPriorities)
}

// TestTriageReview_ReflectsNewVerdicts: the document is output, so a recorded
// verdict has to move the row on the next render.
func TestTriageReview_ReflectsNewVerdicts(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-render-updates", 3, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)

	out, err := tc.RunCampInDir(path, "triage", "review", "--render-only")
	require.NoError(t, err, out)
	before, err := tc.ReadFile(runDir + "/TRIAGE_REVIEW.md")
	require.NoError(t, err)
	assert.Contains(t, before, "Awaiting judgment")

	judgeRow(t, tc, path, ids[0], "completed")
	out, err = tc.RunCampInDir(path, "triage", "review", "--render-only")
	require.NoError(t, err, out)
	after, err := tc.ReadFile(runDir + "/TRIAGE_REVIEW.md")
	require.NoError(t, err)

	assert.NotEqual(t, before, after)
	assert.Contains(t, after, "Close as delivered")
	assert.Contains(t, after, "| `"+ids[0]+"` | completed |")
}

// TestTriageReview_OverwritesAnEditedDocument is D3 made observable: editing
// the rendered file is not an input path, and the next render says so by
// discarding the edit.
func TestTriageReview_OverwritesAnEditedDocument(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-render-overwrite", 2, 0)
	_, runDir := startTriageRun(t, tc, path)

	out, err := tc.RunCampInDir(path, "triage", "review", "--render-only")
	require.NoError(t, err, out)
	tc.Shell(t, "cd "+runDir+" && printf 'APPROVED BY HAND\\n' >> TRIAGE_REVIEW.md")

	edited, err := tc.ReadFile(runDir + "/TRIAGE_REVIEW.md")
	require.NoError(t, err)
	require.Contains(t, edited, "APPROVED BY HAND")

	out, err = tc.RunCampInDir(path, "triage", "review", "--render-only")
	require.NoError(t, err, out)
	rerendered, err := tc.ReadFile(runDir + "/TRIAGE_REVIEW.md")
	require.NoError(t, err)

	assert.NotContains(t, rerendered, "APPROVED BY HAND",
		"an edit to a rendered document is discarded, exactly as the banner warns")
}

// --- priorities --------------------------------------------------------

// TestTriagePriorities_PrintsTheBrief covers the default path.
func TestTriagePriorities_PrintsTheBrief(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-priorities-print", 3, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	judgeRow(t, tc, path, ids[0], "current")

	output, err := tc.RunCampInDir(path, "triage", "priorities")
	require.NoError(t, err, output)

	assert.Contains(t, output, "# Portfolio priorities")
	assert.Contains(t, output, "Primary execution thread")
	assert.Contains(t, output, ids[0])
	assert.Contains(t, output, "Not yet decided", "unjudged rows are named, not hidden")
}

// TestTriagePriorities_ExportWithNoPathConfiguredIsANoOp: the default profile
// sets no export path, and asking for one must report that rather than fail.
func TestTriagePriorities_ExportWithNoPathConfiguredIsANoOp(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-priorities-noexport", 2, 0)
	startTriageRun(t, tc, path)

	output, exitCode, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage priorities --export; echo EXIT:$?")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	assert.Contains(t, output, "EXIT:0", "no export path is not a failure: %s", output)
	assert.Contains(t, output, "No export path set")
	assert.Contains(t, output, "outputs.priorities_export")
}

// TestTriagePriorities_ExportWritesAndOverwrites covers the D3 overwrite rule:
// the export is a rendered copy whose history is git's job.
func TestTriagePriorities_ExportWritesAndOverwrites(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-priorities-export", 3, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)

	// Point the run's embedded profile at an export path, the way the profile
	// layer will once it ships.
	tc.Shell(t, "cd "+runDir+" && jq '.profile.resolved.outputs.priorities_export = "+
		`"current_priorities/PRIORITIES.md"'`+" manifest.json > m.tmp && mv m.tmp manifest.json")

	output, err := tc.RunCampInDir(path, "triage", "priorities", "--export", "--json")
	require.NoError(t, err, output)
	payload := decodeTriageJSON(t, output)
	assert.Equal(t, true, payload["exported"])
	assert.Equal(t, "current_priorities/PRIORITIES.md", payload["export_path"])

	exported, err := tc.ReadFile(path + "/current_priorities/PRIORITIES.md")
	require.NoError(t, err)
	assert.Contains(t, exported, "# Portfolio priorities")
	assert.Contains(t, exported, "Do not edit")

	// Re-exporting after a new verdict overwrites in place rather than
	// versioning: a second file would recreate the stale-brief problem.
	judgeRow(t, tc, path, ids[0], "current")
	output, err = tc.RunCampInDir(path, "triage", "priorities", "--export", "--json")
	require.NoError(t, err, output)

	updated, err := tc.ReadFile(path + "/current_priorities/PRIORITIES.md")
	require.NoError(t, err)
	assert.NotEqual(t, exported, updated)
	assert.Contains(t, updated, "Primary execution thread")

	listing := tc.Shell(t, "ls "+path+"/current_priorities | wc -l")
	assert.Equal(t, "1", strings.TrimSpace(listing), "the export overwrites, never versions")
}

// TestTriagePriorities_ExportRefusesToEscapeTheCampaign: the profile is a file
// a user edits, so its export path is untrusted input reaching a write.
func TestTriagePriorities_ExportRefusesToEscapeTheCampaign(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-priorities-escape", 2, 0)
	_, runDir := startTriageRun(t, tc, path)

	for _, bad := range []string{"../escaped/PRIORITIES.md", "/tmp/escaped.md"} {
		tc.Shell(t, "cd "+runDir+" && jq --arg p "+`"`+bad+`"`+
			" '.profile.resolved.outputs.priorities_export = $p' manifest.json > m.tmp && mv m.tmp manifest.json")

		output, _, err := tc.ExecCommand("sh", "-c",
			"cd "+path+" && /camp triage priorities --export 2>&1 | tail -3; echo EXIT:${PIPESTATUS[0]}")
		require.NoError(t, err)

		assert.Contains(t, output, "priorities_export",
			"the refusal names the offending profile key for %s: %s", bad, output)
	}

	exists, err := tc.CheckFileExists("/tmp/escaped.md")
	require.NoError(t, err)
	assert.False(t, exists, "nothing is written outside the campaign")
}

// TestTriageReview_JSONIsParseableByARealConsumer validates the contract with
// jq rather than only with our own unmarshaler.
func TestTriageReview_JSONIsParseableByARealConsumer(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-render-jq", 3, 1)
	startTriageRun(t, tc, path)

	out := tc.Shell(t, "cd "+path+
		" && /camp triage review --render-only --json | jq -r '.render.rows, .render.lanes, .schema_version'")

	fields := strings.Fields(strings.TrimSpace(out))
	require.Len(t, fields, 3, "jq should read three fields: %s", out)
	assert.Equal(t, "4", fields[0])
	assert.Equal(t, "1", fields[1], "everything is unjudged, so one lane")
	assert.Equal(t, "triage-review/v1alpha1", fields[2])
}
