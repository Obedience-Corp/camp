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

// setupTriageCampaign creates a campaign seeded with count design workitems and
// a handful of intents.
//
// The workitems are written as directories with .workitem markers in one shell
// round trip rather than one `camp workitem create` per item: at 150 items the
// per-invocation container overhead would dominate the runtime of a test whose
// whole point is that the snapshot itself is fast.
func setupTriageCampaign(t *testing.T, tc *TestContainer, name string, designs, intents int) string {
	t.Helper()

	path := "/campaigns/" + name
	out, err := tc.InitCampaign(path, name, "product")
	require.NoError(t, err, "init campaign: %s", out)

	// Designs are directory workitems under workflow/design; intents are
	// markdown files under .campaign/intents/<stage>/. Seeding each where
	// discovery actually looks is the point: a fixture that invented its own
	// layout would pass while the real command found nothing.
	script := fmt.Sprintf(`set -e
cd %s
mkdir -p workflow/design .campaign/intents/inbox
for i in $(seq 1 %d); do
  d=workflow/design/design-item-$i
  mkdir -p "$d"
  printf 'version: v1alpha8\nkind: workitem\nid: design-item-%%s\ntype: design\ntitle: Design item %%s\n' "$i" "$i" > "$d/.workitem"
  printf '# Design item %%s\n\nBody.\n' "$i" > "$d/README.md"
done
for i in $(seq 1 %d); do
  f=.campaign/intents/inbox/intent-item-$i.md
  printf -- '---\nid: intent-item-%%s\ntitle: "Intent item %%s"\ntype: idea\nstatus: inbox\ncreated_at: 2026-08-01T10:00:00-06:00\nauthor: fixture\ntags: []\npriority: medium\nhorizon: later\n---\n\nAn idea worth triaging.\n' "$i" "$i" > "$f"
done
git add -A && git commit -q -m 'seed triage fixture'
`, path, designs, intents)
	tc.Shell(t, script)

	return path
}

// decodeTriageStart parses the --json payload of camp triage start.
func decodeTriageStart(t *testing.T, output string) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &payload),
		"triage start --json should emit one JSON object, got:\n%s", output)
	return payload
}

// TestTriageStart_CreatesRunOnDisk is the sequence's core claim: start writes a
// complete, resumable run under .campaign/triage/.
func TestTriageStart_CreatesRunOnDisk(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-start-basic", 7, 3)

	output, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, "triage start should succeed: %s", output)

	payload := decodeTriageStart(t, output)
	assert.Equal(t, "triage-start/v1alpha1", payload["schema_version"])
	assert.Equal(t, "full", payload["mode"])
	assert.Equal(t, "default", payload["profile"])
	assert.Equal(t, float64(10), payload["rows"], "7 designs + 3 intents")
	assert.Equal(t, float64(10), payload["queued"])
	assert.Equal(t, float64(0), payload["carried"], "a bootstrap run carries nothing")

	runID, ok := payload["run_id"].(string)
	require.True(t, ok, "run_id must be a string")
	assert.True(t, strings.HasPrefix(runID, "run-"), "run id shape: %s", runID)

	runDir := path + "/.campaign/triage/runs/" + runID
	for _, rel := range []string{"/manifest.json", "/run.json", "/WORKFLOW.md"} {
		exists, existsErr := tc.CheckFileExists(runDir + rel)
		require.NoError(t, existsErr)
		assert.True(t, exists, "run should contain %s", rel)
	}

	latest, err := tc.ReadFile(path + "/.campaign/triage/latest")
	require.NoError(t, err)
	assert.Equal(t, runID, strings.TrimSpace(latest))

	state, err := tc.ReadFile(runDir + "/run.json")
	require.NoError(t, err)
	var runState map[string]any
	require.NoError(t, json.Unmarshal([]byte(state), &runState))
	assert.Equal(t, "triage/v1alpha1", runState["schema_version"])
	assert.Equal(t, "snapshotted", runState["phase"])
}

// TestTriageStart_ManifestMatchesSchema checks the frozen snapshot on disk,
// not just the summary the command printed.
func TestTriageStart_ManifestMatchesSchema(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-start-manifest", 6, 2)

	output, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, output)
	runID := decodeTriageStart(t, output)["run_id"].(string)

	raw, err := tc.ReadFile(path + "/.campaign/triage/runs/" + runID + "/manifest.json")
	require.NoError(t, err)

	var manifest struct {
		SchemaVersion string `json:"schema_version"`
		RunID         string `json:"run_id"`
		Mode          string `json:"mode"`
		BaseRunID     *string
		Profile       struct {
			Name     string `json:"name"`
			Resolved struct {
				SchemaVersion string `json:"schema_version"`
				Review        struct {
					BatchSize int    `json:"batch_size"`
					GroupBy   string `json:"group_by"`
				} `json:"review"`
			} `json:"resolved"`
		} `json:"profile"`
		Rows []struct {
			StableID       string `json:"stable_id"`
			Ref            string `json:"ref"`
			Key            string `json:"key"`
			Type           string `json:"type"`
			RelativePath   string `json:"relative_path"`
			LifecycleStage string `json:"lifecycle_stage"`
			Batch          int    `json:"batch"`
			Policy         struct {
				Evidence    string `json:"evidence"`
				RoutingTier string `json:"routing_tier"`
			} `json:"policy"`
			CarriedFrom *string `json:"carried_from"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &manifest))

	assert.Equal(t, "triage/v1alpha1", manifest.SchemaVersion)
	assert.Equal(t, runID, manifest.RunID)
	assert.Equal(t, "full", manifest.Mode)
	assert.Nil(t, manifest.BaseRunID, "a bootstrap run has no base")
	assert.Equal(t, "default", manifest.Profile.Name)
	assert.Equal(t, "triage-profile/v1alpha1", manifest.Profile.Resolved.SchemaVersion,
		"the resolved profile is embedded so the run stays explainable")
	assert.Equal(t, 5, manifest.Profile.Resolved.Review.BatchSize)
	assert.Equal(t, "type", manifest.Profile.Resolved.Review.GroupBy)

	require.Len(t, manifest.Rows, 8)
	for _, row := range manifest.Rows {
		assert.NotEmpty(t, row.StableID, "every row needs an identity")
		assert.NotEmpty(t, row.Key)
		assert.NotEmpty(t, row.RelativePath)
		assert.GreaterOrEqual(t, row.Batch, 1, "every row is batched")
		assert.NotEmpty(t, row.Policy.Evidence)
		assert.NotEmpty(t, row.Policy.RoutingTier)
		assert.Nil(t, row.CarriedFrom, "nothing is carried on a bootstrap run")
	}
}

// TestTriageStart_RefusesWhileRunActive is the exit-2 precondition from spec
// doc 03, checked as a real process exit code rather than an error string.
func TestTriageStart_RefusesWhileRunActive(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-start-conflict", 3, 0)

	first, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, first)
	firstID := decodeTriageStart(t, first)["run_id"].(string)

	output, exitCode, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage start 2>&1; echo EXIT:$?")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "the wrapper shell itself should succeed")

	assert.Contains(t, output, "EXIT:2",
		"an active run is a precondition failure, not a crash: %s", output)
	assert.Contains(t, output, firstID, "the refusal names the run in the way")
	assert.Contains(t, output, "camp triage abandon", "and how to clear it")

	// The refused start must not have created a second run.
	listing := tc.Shell(t, "ls "+path+"/.campaign/triage/runs | wc -l")
	assert.Equal(t, "1", strings.TrimSpace(listing))
}

// TestTriageStart_ScopeNarrowsTheRun covers --scope end to end.
func TestTriageStart_ScopeNarrowsTheRun(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-start-scope", 4, 6)

	output, err := tc.RunCampInDir(path, "triage", "start", "--scope", "type:design", "--json")
	require.NoError(t, err, output)

	payload := decodeTriageStart(t, output)
	assert.Equal(t, float64(4), payload["rows"], "only the design workitems are in scope")
}

// TestTriageStart_RejectsBadScopeExpression: a mistyped scope must not look
// like an empty campaign.
func TestTriageStart_RejectsBadScopeExpression(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-start-badscope", 2, 0)

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage start --scope colour:blue --json 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.NotContains(t, output, "EXIT:0", "a bad scope must fail: %s", output)
	assert.Contains(t, output, "colour")
	exists, err := tc.CheckFileExists(path + "/.campaign/triage/latest")
	require.NoError(t, err)
	assert.False(t, exists, "a rejected start creates no run")
}

// TestTriageStart_EmptyScopeRefuses: opening an empty run would take the single
// active slot and have to be abandoned before the operator could fix the scope.
func TestTriageStart_EmptyScopeRefuses(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-start-empty", 2, 0)

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage start --scope type:explore 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.Contains(t, output, "EXIT:2", "empty scope is a precondition failure: %s", output)
	exists, err := tc.CheckFileExists(path + "/.campaign/triage/latest")
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestTriageStart_IsDeterministic is the sequence's determinism criterion: two
// starts over an identical campaign produce identical manifests once the
// run-scoped identifiers are removed.
func TestTriageStart_IsDeterministic(t *testing.T) {
	tc := GetSharedContainer(t)
	pathA := setupTriageCampaign(t, tc, "triage-determinism-a", 12, 5)
	pathB := setupTriageCampaign(t, tc, "triage-determinism-b", 12, 5)

	manifestA := startAndReadManifest(t, tc, pathA)
	manifestB := startAndReadManifest(t, tc, pathB)

	assert.Equal(t, manifestA, manifestB,
		"identical campaigns must snapshot to identical manifests")
}

// startAndReadManifest starts a run and returns its manifest with the values
// that legitimately differ per run removed.
//
// The sequence's determinism criterion is "identical manifests modulo run id
// and clock-injected timestamps", so the run id, the snapshot time, and any
// timestamp derived from it are normalized. Everything else — every row, its
// identity, its policy, its batch, and the embedded profile — must match
// exactly.
func startAndReadManifest(t *testing.T, tc *TestContainer, path string) string {
	t.Helper()

	output, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, output)
	runID := decodeTriageStart(t, output)["run_id"].(string)

	raw, err := tc.ReadFile(path + "/.campaign/triage/runs/" + runID + "/manifest.json")
	require.NoError(t, err)

	var manifest map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &manifest))
	delete(manifest, "run_id")
	delete(manifest, "created_at")
	normalizeRunTimestamps(manifest)

	normalized, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	return string(normalized)
}

// normalizeRunTimestamps replaces every value that is a run-scoped timestamp
// with a fixed marker, so a comparison is about content rather than when the
// two runs happened to execute.
func normalizeRunTimestamps(node any) {
	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			if str, ok := value.(string); ok && isRunTimestampKey(key) && str != "" {
				typed[key] = "<normalized>"
				continue
			}
			normalizeRunTimestamps(value)
		}
	case []any:
		for _, value := range typed {
			normalizeRunTimestamps(value)
		}
	}
}

func isRunTimestampKey(key string) bool {
	switch key {
	case "granted_at", "created_at", "at", "verdict_at":
		return true
	}
	return false
}

// TestTriageStart_SnapshotsACampaignQuickly is the phase's speed criterion:
// a 150-item campaign snapshots in single-digit seconds.
func TestTriageStart_SnapshotsACampaignQuickly(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-scale", 130, 20)

	started := time.Now()
	output, err := tc.RunCampInDir(path, "triage", "start", "--json")
	elapsed := time.Since(started)
	require.NoError(t, err, output)

	payload := decodeTriageStart(t, output)
	assert.Equal(t, float64(150), payload["rows"])
	assert.Equal(t, float64(30), payload["batches"], "150 rows at batch_size 5")
	assert.Less(t, elapsed, 10*time.Second,
		"snapshot of a 150-item campaign took %s", elapsed)
}

// TestTriageStart_WorkflowDocIsOptional covers the scaffold flag surface.
func TestTriageStart_WorkflowDocIsOptional(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-noworkflow", 2, 0)

	output, err := tc.RunCampInDir(path, "triage", "start", "--no-workflow-doc", "--json")
	require.NoError(t, err, output)

	payload := decodeTriageStart(t, output)
	assert.Equal(t, false, payload["scaffold_workflow_doc"])
	runID := payload["run_id"].(string)

	exists, err := tc.CheckFileExists(path + "/.campaign/triage/runs/" + runID + "/WORKFLOW.md")
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestTriageStart_JSONIsParseableByARealConsumer runs the payload through jq,
// so the contract is validated by something other than our own unmarshaler.
func TestTriageStart_JSONIsParseableByARealConsumer(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-start-jq", 3, 1)

	out := tc.Shell(t, "cd "+path+" && /camp triage start --json | jq -r '.run_id, .rows, .mode'")

	lines := strings.Fields(strings.TrimSpace(out))
	require.Len(t, lines, 3, "jq should read three fields: %s", out)
	assert.True(t, strings.HasPrefix(lines[0], "run-"))
	assert.Equal(t, "4", lines[1])
	assert.Equal(t, "full", lines[2])
}
