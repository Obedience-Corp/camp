//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refreshSummary is the summary block of `camp triage refresh --json`.
type refreshSummary struct {
	Fresh                    int `json:"fresh"`
	Moved                    int `json:"moved"`
	Changed                  int `json:"changed"`
	Gone                     int `json:"gone"`
	New                      int `json:"new"`
	StaleRecorded            int `json:"stale_recorded"`
	RowsWithUncheckedAnchors int `json:"rows_with_unchecked_anchors"`
	RemoteAnchorsResolved    int `json:"remote_anchors_resolved"`
	CarryLost                int `json:"carry_lost"`
}

type refreshPayload struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	Rows          []struct {
		StableID         string `json:"stable_id"`
		Class            string `json:"class"`
		Reason           string `json:"reason"`
		Applicable       bool   `json:"applicable"`
		StaleRecorded    bool   `json:"stale_recorded"`
		Rekeyed          bool   `json:"rekeyed"`
		Appended         bool   `json:"appended"`
		UncheckedAnchors int    `json:"unchecked_anchors"`
	} `json:"rows"`
	CarryLost []struct {
		StableID string `json:"stable_id"`
		Reason   string `json:"reason"`
	} `json:"carry_lost"`
	Summary refreshSummary `json:"summary"`
}

// refresh runs the command and decodes its JSON.
func refresh(t *testing.T, tc *TestContainer, path string) refreshPayload {
	t.Helper()
	output, err := tc.RunCampInDir(path, "triage", "refresh", "--json")
	require.NoError(t, err, output)

	var payload refreshPayload
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &payload))
	return payload
}

// extractJSON trims anything printed before the JSON object.
func extractJSON(t *testing.T, output string) string {
	t.Helper()
	start := -1
	for i, r := range output {
		if r == '{' {
			start = i
			break
		}
	}
	require.GreaterOrEqual(t, start, 0, "no JSON object in output: %s", output)
	return output[start:]
}

// rowByStableID finds one classified row.
func rowByStableID(t *testing.T, payload refreshPayload, stableID string) struct {
	StableID         string `json:"stable_id"`
	Class            string `json:"class"`
	Reason           string `json:"reason"`
	Applicable       bool   `json:"applicable"`
	StaleRecorded    bool   `json:"stale_recorded"`
	Rekeyed          bool   `json:"rekeyed"`
	Appended         bool   `json:"appended"`
	UncheckedAnchors int    `json:"unchecked_anchors"`
} {
	t.Helper()
	for _, row := range payload.Rows {
		if row.StableID == stableID {
			return row
		}
	}
	t.Fatalf("no row %q in refresh output", stableID)
	panic("unreachable")
}

// TestTriageRefresh_UnchangedCampaignIsAllFresh is the baseline through the
// real binary: a refresh over a campaign nobody touched changes nothing.
func TestTriageRefresh_UnchangedCampaignIsAllFresh(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-refresh-baseline", 3, 1)
	_, runDir := startTriageRun(t, tc, path)

	before, err := tc.ReadFile(runDir + "/manifest.json")
	require.NoError(t, err)

	payload := refresh(t, tc, path)

	assert.Equal(t, "triage-refresh/v1alpha1", payload.SchemaVersion)
	assert.Equal(t, 4, payload.Summary.Fresh)
	assert.Equal(t, 0, payload.Summary.Moved+payload.Summary.Changed+
		payload.Summary.Gone+payload.Summary.New)
	assert.Equal(t, 0, payload.Summary.StaleRecorded)

	after, err := tc.ReadFile(runDir + "/manifest.json")
	require.NoError(t, err)
	assert.Equal(t, before, after, "a no-op refresh must not rewrite the manifest")

	for _, row := range payload.Rows {
		assert.True(t, row.Applicable)
		assert.NotEmpty(t, row.Reason, "every row explains its class, including fresh ones")
	}
}

// TestTriageRefresh_RekeysAMovedWorkitem moves a real directory on disk and
// asserts the manifest follows it. This is FT-006 against the filesystem.
func TestTriageRefresh_RekeysAMovedWorkitem(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-refresh-moved", 2, 0)
	_, runDir := startTriageRun(t, tc, path)

	tc.Shell(t, fmt.Sprintf(`set -e
cd %s
mv workflow/design/design-item-1 workflow/design/design-item-1-renamed
`, path))

	payload := refresh(t, tc, path)

	row := rowByStableID(t, payload, "design-item-1")
	assert.Equal(t, "moved", row.Class)
	assert.True(t, row.Rekeyed)
	assert.True(t, row.Applicable, "a move keeps the verdict applicable")
	assert.Contains(t, row.Reason, "design-item-1-renamed")
	assert.Contains(t, row.Reason, "verdict stands")

	manifest, err := tc.ReadFile(runDir + "/manifest.json")
	require.NoError(t, err)
	assert.Contains(t, manifest, "workflow/design/design-item-1-renamed")
	assert.NotContains(t, manifest, `"relative_path": "workflow/design/design-item-1"`)

	// Converges: the second pass sees the re-keyed manifest.
	again := refresh(t, tc, path)
	assert.Equal(t, "fresh", rowByStableID(t, again, "design-item-1").Class)
	assert.Equal(t, 0, again.Summary.Moved)
}

// TestTriageRefresh_RetiresAVerdictWhenAnAnchoredFileChanges is the changed
// class end to end: a real file is edited after the verdict.
func TestTriageRefresh_RetiresAVerdictWhenAnAnchoredFileChanges(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-refresh-anchor", 2, 0)
	_, runDir := startTriageRun(t, tc, path)

	tc.Shell(t, fmt.Sprintf(`set -e
cd %s
mkdir -p docs
printf 'as judged\n' > docs/anchored.md
`, path))

	// An evidence record whose verdict rests on that file.
	evidence := fmt.Sprintf(`{
  "schema_version": "triage/v1alpha1",
  "stable_id": "design-item-1",
  "original_goal": "ship the anchored thing",
  "delivered": ["the anchored thing"],
  "missing": [],
  "stale_assumptions": [],
  "related": [],
  "open_decisions": [],
  "confidence": "high",
  "anchors": [{"kind": "path", "path": "docs/anchored.md", "hash": "%s"}],
  "produced_by": {"role": "evidence", "runtime": "integration-test", "at": "2026-08-05T00:00:00Z"}
}`, sha256Of(t, tc, path+"/docs/anchored.md"))
	require.NoError(t, tc.WriteFile(path+"/evidence.json", evidence))

	out, err := tc.RunCampInDir(path, "triage", "evidence", "set", "design-item-1",
		"--file", "evidence.json", "--json")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir(path, "triage", "propose", "design-item-1",
		"--disposition", "parked", "--summary", "anchored on a file", "--json")
	require.NoError(t, err, out)

	// Nothing moved yet.
	assert.Equal(t, "fresh", rowByStableID(t, refresh(t, tc, path), "design-item-1").Class)

	tc.Shell(t, fmt.Sprintf(`set -e
cd %s
printf 'edited after the verdict\n' > docs/anchored.md
`, path))

	payload := refresh(t, tc, path)

	row := rowByStableID(t, payload, "design-item-1")
	assert.Equal(t, "changed", row.Class)
	assert.False(t, row.Applicable, "apply must refuse a row whose evidence moved")
	assert.True(t, row.StaleRecorded)
	assert.Contains(t, row.Reason, "path:docs/anchored.md")
	assert.Equal(t, 1, payload.Summary.StaleRecorded)

	decisions, err := tc.ReadFile(runDir + "/decisions.jsonl")
	require.NoError(t, err)
	assert.Contains(t, decisions, `"event":"stale"`)
	assert.Contains(t, decisions, "docs/anchored.md")

	// The row is back in the queue rather than silently dropped.
	queue, err := tc.RunCampInDir(path, "triage", "queue", "--json")
	require.NoError(t, err, queue)
	assert.Contains(t, queue, "design-item-1")
}

// TestTriageRefresh_ReportsAGoneWorkitem covers the FT-006
// external-completion story against a real dungeon move.
func TestTriageRefresh_ReportsAGoneWorkitem(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-refresh-gone", 2, 0)
	startTriageRun(t, tc, path)

	tc.Shell(t, fmt.Sprintf(`set -e
cd %s
mkdir -p .dungeon/completed
mv workflow/design/design-item-1 .dungeon/completed/
`, path))

	payload := refresh(t, tc, path)

	row := rowByStableID(t, payload, "design-item-1")
	assert.Equal(t, "gone", row.Class)
	assert.False(t, row.Applicable)
	assert.Contains(t, row.Reason, "external completion")
	assert.Equal(t, 1, payload.Summary.Gone)
}

// TestTriageRefresh_AppendsANewDiscovery covers the fifth class and the batch
// numbering that keeps an in-flight review stable.
func TestTriageRefresh_AppendsANewDiscovery(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-refresh-new", 2, 0)
	_, runDir := startTriageRun(t, tc, path)

	before := manifestRowIDs(t, tc, runDir)

	tc.Shell(t, fmt.Sprintf(`set -e
cd %s
d=workflow/design/design-item-fresh
mkdir -p "$d"
printf 'version: v1alpha8\nkind: workitem\nid: design-item-fresh\ntype: design\ntitle: Fresh design\n' > "$d/.workitem"
printf '# Fresh design\n\nBody.\n' > "$d/README.md"
`, path))

	payload := refresh(t, tc, path)

	row := rowByStableID(t, payload, "design-item-fresh")
	assert.Equal(t, "new", row.Class)
	assert.True(t, row.Appended)
	assert.Contains(t, row.Reason, "absent from the run snapshot")

	after := manifestRowIDs(t, tc, runDir)
	assert.Len(t, after, len(before)+1)
	assert.Contains(t, after, "design-item-fresh")

	// Appended once, not on every pass.
	again := refresh(t, tc, path)
	assert.Equal(t, 0, again.Summary.New)
	assert.Equal(t, "fresh", rowByStableID(t, again, "design-item-fresh").Class)
	assert.Len(t, manifestRowIDs(t, tc, runDir), len(before)+1)
}

// TestTriageRefresh_OfflinePRAnchorIsUncheckedNotAssumed is the offline rule
// through the binary. The container has no gh, so this is the real path a
// user without it takes.
func TestTriageRefresh_OfflinePRAnchorIsUncheckedNotAssumed(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-refresh-offline", 2, 0)
	startTriageRun(t, tc, path)

	evidence := `{
  "schema_version": "triage/v1alpha1",
  "stable_id": "design-item-1",
  "original_goal": "land the PR",
  "delivered": ["the change"],
  "missing": [],
  "stale_assumptions": [],
  "related": [],
  "open_decisions": [],
  "confidence": "high",
  "anchors": [{"kind": "pr", "repo": "Obedience-Corp/camp", "number": 546, "observed": "open"}],
  "produced_by": {"role": "evidence", "runtime": "integration-test", "at": "2026-08-05T00:00:00Z"}
}`
	require.NoError(t, tc.WriteFile(path+"/evidence.json", evidence))
	out, err := tc.RunCampInDir(path, "triage", "evidence", "set", "design-item-1",
		"--file", "evidence.json", "--json")
	require.NoError(t, err, out)

	payload := refresh(t, tc, path)

	row := rowByStableID(t, payload, "design-item-1")
	assert.Equal(t, "fresh", row.Class, "offline must not invalidate a run")
	assert.Equal(t, 1, row.UncheckedAnchors)
	assert.Contains(t, row.Reason, "unchecked")
	assert.Equal(t, 1, payload.Summary.RowsWithUncheckedAnchors)
	assert.Equal(t, 0, payload.Summary.RemoteAnchorsResolved)
	assert.Equal(t, 0, payload.Summary.StaleRecorded)

	// The human output says so rather than staying silent about it.
	human, err := tc.RunCampInDir(path, "triage", "refresh")
	require.NoError(t, err, human)
	assert.Contains(t, human, "could not be re-checked")
	assert.Contains(t, human, "rather than assumed current")
}

// TestTriageRefresh_HonorsTheRunsFrozenScope is the scope_expressions field
// doing its job through the binary: a run started with --scope must not adopt
// the rest of the campaign on its next refresh.
func TestTriageRefresh_HonorsTheRunsFrozenScope(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-refresh-scoped", 2, 2)

	out, err := tc.RunCampInDir(path, "triage", "start", "--scope", "type:design", "--json")
	require.NoError(t, err, out)

	payload := refresh(t, tc, path)

	assert.Equal(t, 0, payload.Summary.New,
		"the intents are outside a type:design run and are not new discoveries")
	assert.Equal(t, 2, payload.Summary.Fresh)
	for _, row := range payload.Rows {
		assert.NotContains(t, row.StableID, "intent-item")
	}
}

// TestTriageRefresh_RejectsAnUnknownRun keeps the error path honest.
func TestTriageRefresh_RejectsAnUnknownRun(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-refresh-badrun", 1, 0)
	startTriageRun(t, tc, path)

	output, err := tc.RunCampInDir(path, "triage", "refresh", "--run", "run-nope")
	require.Error(t, err, output)
	assert.Contains(t, output, "run-nope")
}

// TestTriageRefresh_JSONIsParseableByARealConsumer pins the machine contract.
func TestTriageRefresh_JSONIsParseableByARealConsumer(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-refresh-json", 2, 0)
	startTriageRun(t, tc, path)

	output, err := tc.RunCampInDir(path, "triage", "refresh", "--json")
	require.NoError(t, err, output)

	var generic map[string]any
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &generic))

	for _, key := range []string{"schema_version", "run_id", "rows", "carry_lost", "summary"} {
		assert.Contains(t, generic, key, "the envelope must carry %q", key)
	}
	// Nil slices marshal to null and break naive consumers; these must be [].
	assert.NotNil(t, generic["carry_lost"])
	assert.IsType(t, []any{}, generic["carry_lost"])

	summary := generic["summary"].(map[string]any)
	for _, key := range []string{
		"fresh", "moved", "changed", "gone", "new", "stale_recorded",
		"rows_with_unchecked_anchors", "remote_anchors_resolved", "carry_lost",
	} {
		assert.Contains(t, summary, key, "the summary must carry %q even at zero", key)
	}
}

// sha256Of hashes a file inside the container in the anchor's recorded format.
func sha256Of(t *testing.T, tc *TestContainer, path string) string {
	t.Helper()
	out := tc.Shell(t, "sha256sum "+path+" | cut -d' ' -f1")
	return "sha256:" + trimSpace(out)
}

// trimSpace drops surrounding whitespace without pulling in strings for one call.
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\n' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\n' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
