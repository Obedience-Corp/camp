//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// approveAll judges and approves every row in the run so apply has work.
func approveAll(t *testing.T, tc *TestContainer, path string, ids []string, disposition string) {
	t.Helper()
	for _, id := range ids {
		judgeRow(t, tc, path, id, disposition)
	}
	// Terminal dispositions cannot be bulk-approved: approving one retires a
	// workitem, so it has to be named. That refusal is the phase-002 safety
	// rule working, so the helper names each row rather than routing around it.
	for _, id := range ids {
		out, err := tc.RunCampInDir(path, "triage", "approve", id, "--json")
		require.NoError(t, err, out)
	}
}

// applyJSON runs apply and decodes the envelope.
func applyJSON(t *testing.T, tc *TestContainer, path string, args ...string) map[string]any {
	t.Helper()
	full := append([]string{"triage", "apply", "--json"}, args...)
	output, err := tc.RunCampInDir(path, full...)
	require.NoError(t, err, output)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &payload))
	return payload
}

// attentionStageOf reads a workitem's attention stage back out of camp.
func attentionStageOf(t *testing.T, tc *TestContainer, path, stableID string) string {
	t.Helper()
	output, err := tc.RunCampInDir(path, "workitem", "--json", "--show-parked")
	require.NoError(t, err, output)

	// The list key is "items", not "workitems" — verified against real
	// `camp workitem --json` output rather than assumed.
	var payload struct {
		Items []struct {
			StableID       string `json:"stable_id"`
			AttentionStage string `json:"attention_stage"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &payload))
	for _, item := range payload.Items {
		if item.StableID == stableID {
			return item.AttentionStage
		}
	}
	return ""
}

// TestTriageApply_DryRunChangesNothing is the read path.
func TestTriageApply_DryRunChangesNothing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-apply-dryrun", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	approveAll(t, tc, path, manifestRowIDs(t, tc, runDir), "parked")

	output, err := tc.RunCampInDir(path, "triage", "apply", "--dry-run")
	require.NoError(t, err, output)

	assert.Contains(t, output, "camp workitem stage")
	assert.Contains(t, output, "undo:")
	assert.Contains(t, output, "Nothing was changed")

	// The fixture seeds items as active; a dry-run must leave that alone.
	assert.Equal(t, "active", attentionStageOf(t, tc, path, "design-item-1"),
		"a dry-run must not touch the workitem store")
	_, err = tc.ReadFile(runDir + "/receipts.jsonl")
	require.Error(t, err, "a dry-run writes no receipts")
}

// TestTriageApply_MovesRealWorkitemsAndWritesReceipts is the core deliverable:
// verdicts become campaign reality, and the writes are verified by reading
// them back through camp rather than by trusting apply's own output.
func TestTriageApply_MovesRealWorkitemsAndWritesReceipts(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-apply-real", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	approveAll(t, tc, path, manifestRowIDs(t, tc, runDir), "parked")

	payload := applyJSON(t, tc, path)

	applied, _ := payload["applied"].([]any)
	assert.Len(t, applied, 2)
	assert.Equal(t, false, payload["halted"])

	// The write, read back through camp.
	assert.Equal(t, "parked", attentionStageOf(t, tc, path, "design-item-1"))
	assert.Equal(t, "parked", attentionStageOf(t, tc, path, "design-item-2"))

	receipts, err := tc.ReadFile(runDir + "/receipts.jsonl")
	require.NoError(t, err)
	lines := nonEmptyLines(receipts)
	require.Len(t, lines, 2)
	for _, line := range lines {
		assert.Contains(t, line, `"result":"applied"`)
		assert.Contains(t, line, `"argv":["camp","workitem","stage"`)
		assert.Contains(t, line, `"undo":"camp workitem stage`,
			"the receipt carries a command that actually reverses this")
	}
}

// TestTriageApply_ReRunIsIdempotent covers resume: applied rows never execute
// a second time.
func TestTriageApply_ReRunIsIdempotent(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-apply-rerun", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	approveAll(t, tc, path, manifestRowIDs(t, tc, runDir), "parked")

	first := applyJSON(t, tc, path)
	firstApplied, _ := first["applied"].([]any)
	require.Len(t, firstApplied, 2)

	before, err := tc.ReadFile(runDir + "/receipts.jsonl")
	require.NoError(t, err)

	// Exit 2: nothing could be applied because everything already was.
	output, err := tc.RunCampInDir(path, "triage", "apply")
	require.Error(t, err, output)
	assert.Contains(t, output, "already applied")

	after, err := tc.ReadFile(runDir + "/receipts.jsonl")
	require.NoError(t, err)
	assert.Equal(t, before, after, "a re-run appends no receipts for applied rows")
	assert.Equal(t, "parked", attentionStageOf(t, tc, path, "design-item-1"))
}

// TestTriageApply_KillBetweenRowsLeavesReceiptsConsistent is the kill test:
// SIGKILL mid-apply, then a re-run completes and the receipts still parse.
func TestTriageApply_KillBetweenRowsLeavesReceiptsConsistent(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-apply-kill", 4, 0)
	_, runDir := startTriageRun(t, tc, path)
	approveAll(t, tc, path, manifestRowIDs(t, tc, runDir), "parked")

	// Kill the apply almost immediately. Whatever it managed to write must
	// still be a valid receipt stream, which is the property that matters:
	// a torn final line would make every later run fail to read its own
	// history.
	tc.Shell(t, fmt.Sprintf(`set -e
cd %s
camp triage apply >/dev/null 2>&1 &
pid=$!
sleep 0.15
kill -9 $pid 2>/dev/null || true
wait $pid 2>/dev/null || true
exit 0
`, path))

	// The run is legible: either no receipts yet, or well-formed ones.
	if raw, err := tc.ReadFile(runDir + "/receipts.jsonl"); err == nil {
		for i, line := range nonEmptyLines(raw) {
			var receipt map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &receipt),
				"receipt %d survived the kill as valid JSON", i+1)
			assert.NotEmpty(t, receipt["stable_id"])
			assert.NotEmpty(t, receipt["result"])
		}
	}

	// The re-run completes the work.
	output, err := tc.RunCampInDir(path, "triage", "apply")
	require.NoError(t, err, output)

	for _, id := range []string{"design-item-1", "design-item-2", "design-item-3", "design-item-4"} {
		assert.Equal(t, "parked", attentionStageOf(t, tc, path, id),
			"%s must be applied after the resume", id)
	}

	// And every receipt still parses after both passes.
	raw, err := tc.ReadFile(runDir + "/receipts.jsonl")
	require.NoError(t, err)
	seen := map[string]int{}
	for _, line := range nonEmptyLines(raw) {
		var receipt struct {
			StableID string `json:"stable_id"`
			Result   string `json:"result"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &receipt))
		if receipt.Result == "applied" {
			seen[receipt.StableID]++
		}
	}
	for id, count := range seen {
		assert.Equal(t, 1, count, "%s must be applied exactly once across both passes", id)
	}
}

// TestTriageApply_FailureStopsAndReports is the failure-injection test: a
// colliding destination makes one promote fail, and apply must stop rather
// than continue into rows that may depend on it.
func TestTriageApply_FailureStopsAndReports(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-apply-failure", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	ids := manifestRowIDs(t, tc, runDir)
	approveAll(t, tc, path, ids, "completed")

	// Occupy the dungeon destination the first promote will want, so the move
	// collides on a real filesystem rather than on an injected error.
	tc.Shell(t, fmt.Sprintf(`set -e
cd %s
mkdir -p "workflow/design/.dungeon/completed/$(date -u +%%Y-%%m-%%d)/design-item-1"
printf 'occupied\n' > "workflow/design/.dungeon/completed/$(date -u +%%Y-%%m-%%d)/design-item-1/README.md"
git add -A && git commit -q -m 'occupy the destination'
`, path))

	output, _ := tc.RunCampInDir(path, "triage", "apply")

	// Either it halted on the collision, or it refused the row outright.
	// Both are acceptable; silently applying over the collision is not.
	assert.True(t,
		strings.Contains(output, "Stopped at") || strings.Contains(output, "skipped"),
		"apply must report rather than continue past a failed move: %s", output)

	if raw, err := tc.ReadFile(runDir + "/receipts.jsonl"); err == nil {
		for _, line := range nonEmptyLines(raw) {
			if strings.Contains(line, `"result":"failed"`) {
				assert.Contains(t, line, `"error"`,
					"a failed receipt has to say why")
				assert.NotContains(t, line, `"undo"`,
					"a failed action has nothing to undo")
			}
		}
	}
}

// TestTriageApply_RefusesWithoutApprovedVerdicts covers the empty plan.
func TestTriageApply_RefusesWithoutApprovedVerdicts(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-apply-empty", 2, 0)
	startTriageRun(t, tc, path)

	output, err := tc.RunCampInDir(path, "triage", "apply", "--dry-run")
	require.NoError(t, err, output)
	assert.Contains(t, output, "no row carries an approved verdict")
	assert.Contains(t, output, "camp triage approve")
}

// TestTriageApply_JSONIsParseableByARealConsumer pins the machine contract.
func TestTriageApply_JSONIsParseableByARealConsumer(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-apply-json", 2, 0)
	_, runDir := startTriageRun(t, tc, path)
	approveAll(t, tc, path, manifestRowIDs(t, tc, runDir), "parked")

	payload := applyJSON(t, tc, path, "--dry-run")

	for _, key := range []string{"schema_version", "run_id", "dry_run", "plan", "skipped", "receipts"} {
		assert.Contains(t, payload, key, "the envelope must carry %q", key)
	}
	assert.Equal(t, "triage-apply/v1alpha1", payload["schema_version"])
	assert.Equal(t, true, payload["dry_run"])

	// Nil slices marshal to null and break naive consumers.
	assert.IsType(t, []any{}, payload["skipped"])
	assert.IsType(t, []any{}, payload["receipts"])

	plan := payload["plan"].(map[string]any)
	entries := plan["entries"].([]any)
	require.NotEmpty(t, entries)
	entry := entries[0].(map[string]any)
	for _, key := range []string{"stable_id", "commands", "preconditions", "undo"} {
		assert.Contains(t, entry, key)
	}
}

// nonEmptyLines splits a jsonl body into its records.
func nonEmptyLines(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
