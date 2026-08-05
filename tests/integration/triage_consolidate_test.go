//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// proposeConsolidate records a consolidate verdict whose rationale declares
// the successors, which is where the apply plan reads them from.
func proposeConsolidate(t *testing.T, tc *TestContainer, path, stableID string, successors []string) {
	t.Helper()

	out, err := tc.RunCampInDir(path, "triage", "evidence", "set", stableID, "--no-evidence", "--json")
	require.NoError(t, err, out)

	rationale := map[string]any{
		"schema_version": "triage/v1alpha1",
		"summary":        "three asks in one umbrella; split them out",
		"anchors_used":   []string{},
		"confidence":     "high",
		"successors":     successors,
	}
	body, err := json.Marshal(rationale)
	require.NoError(t, err)
	require.NoError(t, tc.WriteFile(path+"/rationale.json", string(body)))

	out, err = tc.RunCampInDir(path, "triage", "propose", stableID,
		"--disposition", "consolidate", "--file", "rationale.json", "--json")
	require.NoError(t, err, out)
}

// judgeRemaining parks every other row, because the review gate refuses to
// open until the whole run is judged. That refusal is correct; the fixture
// satisfies it rather than bypassing it.
func judgeRemaining(t *testing.T, tc *TestContainer, path, runDir, except string) {
	t.Helper()
	for _, id := range manifestRowIDs(t, tc, runDir) {
		if id == except {
			continue
		}
		judgeRow(t, tc, path, id, "parked")
	}
}

// TestTriageConsolidate_EndToEnd is the sequence's Done When: an approved
// consolidate verdict applied end to end. Split runs, the successors exist in
// the same transaction so the gate lets the parent through, the promote lands,
// and verify proves the lineage.
func TestTriageConsolidate_EndToEnd(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-consolidate-e2e", 2, 0)
	_, runDir := startTriageRun(t, tc, path)

	proposeConsolidate(t, tc, path, "design-item-1", []string{"fest-ingest", "fest-registry"})
	judgeRemaining(t, tc, path, runDir, "design-item-1")

	// Terminal dispositions cannot be bulk-approved: a consolidation retires a
	// workitem, so it has to be named. That refusal is the point.
	// A bulk selector never retires or splits a workitem: the consolidate row
	// is skipped and named, whether or not other rows in the batch succeed.
	out, _ := tc.RunCampInDir(path, "triage", "approve", "--batch", "1")
	assert.Contains(t, out, "design-item-1",
		"a consolidate row must be named, not covered by --batch")

	out, err := tc.RunCampInDir(path, "triage", "approve", "design-item-1", "--json")
	require.NoError(t, err, out)

	// The plan now compiles a real split rather than a blocked entry.
	out, err = tc.RunCampInDir(path, "triage", "apply", "--dry-run")
	require.NoError(t, err, out)
	assert.Contains(t, out, "camp workitem split design-item-1")
	assert.Contains(t, out, "--into fest-ingest")
	assert.Contains(t, out, "camp workitem promote design-item-1 --target completed")
	assert.NotContains(t, out, "requires camp workitem split",
		"the verb has landed, so the row is no longer blocked")

	out, err = tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, out)

	// Both successors exist, stamped back to the parent.
	for _, name := range []string{"fest-ingest", "fest-registry"} {
		marker, readErr := tc.ReadFile(path + "/workflow/design/" + name + "/.workitem")
		require.NoError(t, readErr, "%s must have been created", name)
		assert.Contains(t, marker, "split_from: design-item-1")
	}

	// The parent retired: the gate was satisfied inside the same apply,
	// because the split ran before the promote.
	parentMarker, err := tc.ReadFile(path + "/workflow/design/design-item-1/.workitem")
	if err == nil {
		assert.NotContains(t, parentMarker, "kind: workitem",
			"the parent should have moved into the dungeon")
	}
	// Both spellings: a campaign migrated by `camp dungeon migrate` uses the
	// hidden .dungeon, an older one uses dungeon/.
	dungeoned := tc.Shell(t, "ls "+path+"/workflow/design/.dungeon/completed/*/ "+path+"/workflow/design/dungeon/completed/*/ 2>/dev/null || true")
	assert.Contains(t, dungeoned, "design-item-1", "the parent retired after its successors existed")

	// Receipts show the ordering the plan promised.
	receipts, err := tc.ReadFile(runDir + "/receipts.jsonl")
	require.NoError(t, err)
	lines := nonEmptyLines(receipts)
	require.GreaterOrEqual(t, len(lines), 2)
	assert.Contains(t, lines[0], `"kind":"split"`)
	assert.Contains(t, lines[1], `"kind":"dungeon"`)
	assert.Contains(t, lines[0], `"undo":"camp workitem split design-item-1 --undo"`)

	// And verify proves it.
	out, err = tc.RunCampInDir(path, "triage", "verify")
	require.NoError(t, err, out)
	assert.Contains(t, out, "0 mismatched")
}

// TestTriageStatus_ConsolidationQueue is FT-014's work queue: parent, declared
// successors, which are missing, and whether retirement is blocked.
func TestTriageStatus_ConsolidationQueue(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-consolidate-queue", 2, 0)
	_, runDir := startTriageRun(t, tc, path)

	proposeConsolidate(t, tc, path, "design-item-1", []string{"fest-ingest", "design-item-2"})
	judgeRemaining(t, tc, path, runDir, "design-item-1")
	out, err := tc.RunCampInDir(path, "triage", "approve", "design-item-1", "--json")
	require.NoError(t, err, out)

	// Before apply: one successor exists already, the other does not.
	out, err = tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, out)

	var payload struct {
		Run struct {
			Consolidations []struct {
				StableID   string   `json:"stable_id"`
				Successors []string `json:"successors"`
				Missing    []string `json:"missing"`
			} `json:"consolidations"`
		} `json:"run"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, out)), &payload))
	require.Len(t, payload.Run.Consolidations, 1)

	queue := payload.Run.Consolidations[0]
	assert.Equal(t, "design-item-1", queue.StableID)
	assert.Equal(t, []string{"design-item-2", "fest-ingest"}, queue.Successors,
		"sorted, so the queue reads the same every time")
	assert.Equal(t, []string{"fest-ingest"}, queue.Missing,
		"design-item-2 already exists; only the uncreated one is missing")

	// After apply, nothing is missing.
	out, err = tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, out)

	out, err = tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, out)
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, out)), &payload))
	require.Len(t, payload.Run.Consolidations, 1)
	assert.Empty(t, payload.Run.Consolidations[0].Missing,
		"every successor exists after the split ran")
}

// TestTriageStatus_NoConsolidationsWhenNoneProposed keeps the section empty
// rather than absent, so a consumer can index it unconditionally.
func TestTriageStatus_NoConsolidationsWhenNoneProposed(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-consolidate-none", 2, 0)
	startTriageRun(t, tc, path)

	out, err := tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, out)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, out)), &payload))
	run := payload["run"].(map[string]any)
	assert.IsType(t, []any{}, run["consolidations"],
		"an empty queue is [], never null")
}
