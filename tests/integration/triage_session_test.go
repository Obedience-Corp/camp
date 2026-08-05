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

// seedUnadoptedDesigns adds design directories with no .workitem marker: the
// FT-008 population the identity preflight exists for. They are discovered by
// location, so they appear in a run but have no durable identity until adopted.
func seedUnadoptedDesigns(t *testing.T, tc *TestContainer, path string, slugs ...string) {
	t.Helper()

	var b strings.Builder
	b.WriteString("set -e\ncd " + path + "\n")
	for _, slug := range slugs {
		b.WriteString(fmt.Sprintf(
			"mkdir -p workflow/design/%s\nprintf '# %s\\n\\nLegacy, never adopted.\\n' > workflow/design/%s/README.md\n",
			slug, slug, slug))
	}
	b.WriteString("git add -A && git commit -q -m 'seed unadopted designs'\n")
	tc.Shell(t, b.String())
}

func decodeTriageJSON(t *testing.T, output string) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &payload),
		"expected one JSON object, got:\n%s", output)
	return payload
}

// --- identity preflight ------------------------------------------------

// TestTriagePreflight_RepairsAndReports is the default policy: camp closes the
// identity gap itself and says exactly what it did. Silence would leave the
// operator with markers they never wrote.
func TestTriagePreflight_RepairsAndReports(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-preflight-repair", 2, 0)
	seedUnadoptedDesigns(t, tc, path, "legacy-alpha", "legacy-beta")

	output, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, output)

	payload := decodeTriageJSON(t, output)
	assert.Equal(t, float64(4), payload["rows"], "2 adopted + 2 legacy designs")

	repaired, ok := payload["repaired"].([]any)
	require.True(t, ok, "repaired must always be present: %s", output)
	require.Len(t, repaired, 2)

	paths := map[string]map[string]any{}
	for _, entry := range repaired {
		row := entry.(map[string]any)
		paths[row["relative_path"].(string)] = row
	}
	for _, slug := range []string{"legacy-alpha", "legacy-beta"} {
		row, found := paths["workflow/design/"+slug]
		require.True(t, found, "repair for %s should be reported: %s", slug, output)
		assert.NotEmpty(t, row["id"])
		assert.True(t, strings.HasPrefix(row["ref"].(string), "WI-"),
			"ref should follow camp's shape: %v", row["ref"])
	}

	// The marker must really be on disk, and readable by camp's own reader.
	for _, slug := range []string{"legacy-alpha", "legacy-beta"} {
		marker, readErr := tc.ReadFile(path + "/workflow/design/" + slug + "/.workitem")
		require.NoError(t, readErr, "adoption should have written a .workitem for %s", slug)
		assert.Contains(t, marker, "kind: workitem")
		assert.Contains(t, marker, "type: design")
	}

	// And the run must record the repaired identity rather than a path fallback.
	assert.Equal(t, float64(0), payload["identity_exceptions"],
		"nothing should remain path-bound after a successful repair")
}

// TestTriagePreflight_RepairIsReportedInTextOutput: the non-JSON path is the
// one a human sees, so it must name the adoptions too.
func TestTriagePreflight_RepairIsReportedInTextOutput(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-preflight-text", 1, 0)
	seedUnadoptedDesigns(t, tc, path, "legacy-gamma")

	output, err := tc.RunCampInDir(path, "triage", "start")
	require.NoError(t, err, output)

	assert.Contains(t, output, "Adopted 1 workitem")
	assert.Contains(t, output, "workflow/design/legacy-gamma")
	assert.Contains(t, output, "ref: WI-")
}

// TestTriagePreflight_StrictRefusesAndListsPaths: strict makes adoption a
// deliberate act, and the refusal lists everything so it can be fixed in one
// pass.
func TestTriagePreflight_StrictRefusesAndListsPaths(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-preflight-strict", 1, 0)
	seedUnadoptedDesigns(t, tc, path, "legacy-one", "legacy-two")

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage start --identity strict 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.Contains(t, output, "EXIT:2", "strict identity is a precondition failure: %s", output)
	assert.Contains(t, output, "workflow/design/legacy-one")
	assert.Contains(t, output, "workflow/design/legacy-two")
	assert.Contains(t, output, "camp workitem adopt")

	// A refused run must leave the campaign untouched: no run, no markers.
	exists, err := tc.CheckFileExists(path + "/.campaign/triage/latest")
	require.NoError(t, err)
	assert.False(t, exists, "strict refusal creates no run")
	exists, err = tc.CheckFileExists(path + "/workflow/design/legacy-one/.workitem")
	require.NoError(t, err)
	assert.False(t, exists, "strict refusal adopts nothing")
}

// TestTriagePreflight_AdoptedItemMatchesCampWorkitemAdopt proves the two
// routes produce the same thing: triage must not invent a second kind of
// adopted workitem.
func TestTriagePreflight_AdoptedItemMatchesCampWorkitemAdopt(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-preflight-parity", 1, 0)
	seedUnadoptedDesigns(t, tc, path, "by-triage", "by-command")

	out, err := tc.RunCampInDir(path, "workitem", "adopt", "workflow/design/by-command", "--type", "design")
	require.NoError(t, err, "camp workitem adopt should succeed: %s", out)

	out, err = tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, out)

	// Both markers carry the same required fields. Triage additionally records
	// the title discovery already resolved, which `adopt` leaves blank unless
	// the operator passes --title; that is a difference in what each route
	// knows, not in what it produces.
	byCommand, err := tc.ReadFile(path + "/workflow/design/by-command/.workitem")
	require.NoError(t, err)
	byTriage, err := tc.ReadFile(path + "/workflow/design/by-triage/.workitem")
	require.NoError(t, err)
	for _, required := range []string{"version: v1alpha8", "kind: workitem", "type: design"} {
		assert.Contains(t, byCommand, required)
		assert.Contains(t, byTriage, required)
	}

	// The parity that actually matters: camp itself must see both as adopted,
	// each with a stable id and a ref. That is the property every later
	// mutation depends on.
	listing, err := tc.RunCampInDir(path, "workitem", "--json")
	require.NoError(t, err, listing)

	var payload struct {
		Items []struct {
			RelativePath string `json:"relative_path"`
			StableID     string `json:"stable_id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(listing), &payload))

	ids := map[string]string{}
	for _, item := range payload.Items {
		ids[item.RelativePath] = item.StableID
	}
	for _, slug := range []string{"by-command", "by-triage"} {
		id, found := ids["workflow/design/"+slug]
		require.True(t, found, "camp should discover %s", slug)
		assert.NotEmpty(t, id, "%s must have a stable id after adoption", slug)
	}
}

// --- status ------------------------------------------------------------

// TestTriageStatus_ReportsTheActiveRun covers the normal case.
func TestTriageStatus_ReportsTheActiveRun(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-status-active", 6, 2)

	start, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, start)
	runID := decodeTriageJSON(t, start)["run_id"].(string)

	output, err := tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, output)

	payload := decodeTriageJSON(t, output)
	assert.Equal(t, "triage-status/v1alpha1", payload["schema_version"])
	assert.Equal(t, true, payload["has_run"])

	run := payload["run"].(map[string]any)
	assert.Equal(t, runID, run["run_id"])
	assert.Equal(t, "created", run["phase"])
	assert.Equal(t, true, run["active"])
	assert.Equal(t, float64(8), run["rows"])

	counts := run["counts"].(map[string]any)
	assert.Equal(t, float64(8), counts["pending-evidence"],
		"a fresh run has decided nothing")
	assert.Equal(t, float64(0), counts["approved"])
	for _, state := range []string{
		"pending-evidence", "proposed", "approved", "rejected",
		"stale", "applied", "verified", "carried",
	} {
		_, present := counts[state]
		assert.True(t, present, "counts must always carry %s", state)
	}

	assert.NotNil(t, run["batches"])
	consolidations, ok := run["consolidations"].([]any)
	require.True(t, ok, "the consolidation queue must be present from day one")
	assert.Empty(t, consolidations)
}

// TestTriageStatus_ExitsZeroWithNoRun: a campaign that has not triaged yet is
// a state, not an error, and an agent polling status must not treat it as one.
func TestTriageStatus_ExitsZeroWithNoRun(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-status-norun", 2, 0)

	output, exitCode, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage status --json; echo EXIT:$?")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	assert.Contains(t, output, "EXIT:0", "no run is not a failure: %s", output)
	body := strings.SplitN(output, "EXIT:", 2)[0]
	payload := decodeTriageJSON(t, body)
	assert.Equal(t, false, payload["has_run"])
}

// TestTriageStatus_ReadsTheSessionNotTheCampaign is the distinction the help
// text promises: status reports the frozen run, so a campaign that grew after
// the snapshot does not change the answer. That is refresh's job.
func TestTriageStatus_ReadsTheSessionNotTheCampaign(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-status-frozen", 3, 0)

	start, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, start)
	assert.Equal(t, float64(3), decodeTriageJSON(t, start)["rows"])

	seedUnadoptedDesigns(t, tc, path, "added-after-snapshot")

	output, err := tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, output)

	run := decodeTriageJSON(t, output)["run"].(map[string]any)
	assert.Equal(t, float64(3), run["rows"],
		"status reports the frozen snapshot, not the live campaign")
}

// --- abandon -----------------------------------------------------------

// TestTriageAbandon_KeepsStateAndFreesTheSlot is the whole contract of
// abandon: nothing is deleted, and a new run can start.
func TestTriageAbandon_KeepsStateAndFreesTheSlot(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-abandon-basic", 4, 1)

	start, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, start)
	firstID := decodeTriageJSON(t, start)["run_id"].(string)

	output, err := tc.RunCampInDir(path, "triage", "abandon", "--reason", "scope was wrong", "--json")
	require.NoError(t, err, output)

	payload := decodeTriageJSON(t, output)
	assert.Equal(t, firstID, payload["run_id"])
	assert.Equal(t, "abandoned", payload["phase"])
	assert.Equal(t, "scope was wrong", payload["reason"])

	// Nothing deleted.
	for _, rel := range []string{"/manifest.json", "/run.json"} {
		exists, existsErr := tc.CheckFileExists(path + "/.campaign/triage/runs/" + firstID + rel)
		require.NoError(t, existsErr)
		assert.True(t, exists, "abandoning keeps %s", rel)
	}

	// Status still answers for the abandoned run.
	status, err := tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, status)
	run := decodeTriageJSON(t, status)["run"].(map[string]any)
	assert.Equal(t, "abandoned", run["phase"])
	assert.Equal(t, false, run["active"])
	assert.Equal(t, "scope was wrong", run["abandon_reason"])
	assert.Equal(t, float64(5), run["rows"], "the snapshot survives abandonment")

	// The slot is free.
	second, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, "abandoning must free the active slot: %s", second)
	assert.NotEqual(t, firstID, decodeTriageJSON(t, second)["run_id"])
}

// TestTriageAbandon_WithNoRunRefuses: there is nothing to close.
func TestTriageAbandon_WithNoRunRefuses(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-abandon-norun", 2, 0)

	output, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && /camp triage abandon 2>&1; echo EXIT:$?")
	require.NoError(t, err)

	assert.Contains(t, output, "EXIT:2", "nothing to abandon is a precondition failure: %s", output)
}

// TestTriageSession_CreateAppendAbandonCycle is the run-store cycle carried
// forward from sequence 01, now that a CLI verb exists to drive it. It
// exercises the store through the real binary in the container: create a run,
// append a decision to its stream, close it, and confirm every file and the
// latest pointer survive.
func TestTriageSession_CreateAppendAbandonCycle(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-store-cycle", 3, 1)

	// create
	start, err := tc.RunCampInDir(path, "triage", "start", "--json")
	require.NoError(t, err, start)
	runID := decodeTriageJSON(t, start)["run_id"].(string)
	runDir := path + "/.campaign/triage/runs/" + runID

	latest, err := tc.ReadFile(path + "/.campaign/triage/latest")
	require.NoError(t, err)
	require.Equal(t, runID, strings.TrimSpace(latest))

	// append: write a decision line the way the store's JSONL contract
	// specifies, then prove camp reads it back through status's fold.
	stableID := firstManifestStableID(t, tc, runDir)
	decision := fmt.Sprintf(
		`{"schema_version":"triage/v1alpha1","event":"approved","stable_id":%q,`+
			`"disposition":"parked","canonical_action":"attention/parked",`+
			`"actor":"fixture","at":"2026-08-10T14:00:00Z"}`, stableID)
	tc.Shell(t, "cd "+runDir+" && printf '%s\\n' '"+decision+"' >> decisions.jsonl")

	status, err := tc.RunCampInDir(path, "triage", "status", "--json")
	require.NoError(t, err, status)
	counts := decodeTriageJSON(t, status)["run"].(map[string]any)["counts"].(map[string]any)
	assert.Equal(t, float64(1), counts["approved"],
		"the appended verdict must fold into status: %s", status)

	// abandon
	out, err := tc.RunCampInDir(path, "triage", "abandon", "--reason", "cycle complete", "--json")
	require.NoError(t, err, out)
	assert.Equal(t, "abandoned", decodeTriageJSON(t, out)["phase"])

	// Everything survives, including the appended stream.
	for _, rel := range []string{"/manifest.json", "/run.json", "/decisions.jsonl", "/WORKFLOW.md"} {
		exists, existsErr := tc.CheckFileExists(runDir + rel)
		require.NoError(t, existsErr)
		assert.True(t, exists, "the abandoned run keeps %s", rel)
	}
	stream, err := tc.ReadFile(runDir + "/decisions.jsonl")
	require.NoError(t, err)
	assert.Contains(t, stream, stableID)

	state, err := tc.ReadFile(runDir + "/run.json")
	require.NoError(t, err)
	var runState map[string]any
	require.NoError(t, json.Unmarshal([]byte(state), &runState))
	assert.Equal(t, "abandoned", runState["phase"])
	assert.Equal(t, "cycle complete", runState["abandon_reason"])

	history, ok := runState["phase_history"].([]any)
	require.True(t, ok)
	require.GreaterOrEqual(t, len(history), 2, "the history explains how the run reached its phase")
	assert.Equal(t, "created", history[0].(map[string]any)["phase"])
	assert.Equal(t, "abandoned", history[len(history)-1].(map[string]any)["phase"])
}

// firstManifestStableID reads a run's first row id, so a test can address a
// real row without hardcoding a generated id.
func firstManifestStableID(t *testing.T, tc *TestContainer, runDir string) string {
	t.Helper()

	raw, err := tc.ReadFile(runDir + "/manifest.json")
	require.NoError(t, err)

	var manifest struct {
		Rows []struct {
			StableID string `json:"stable_id"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &manifest))
	require.NotEmpty(t, manifest.Rows)
	return manifest.Rows[0].StableID
}
