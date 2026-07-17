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

type validateFinding struct {
	Code          string `json:"code"`
	Severity      string `json:"severity"`
	Target        string `json:"target"`
	Message       string `json:"message"`
	RepairCommand string `json:"repair_command"`
	Repairable    bool   `json:"repairable"`
}

type validateReport struct {
	SchemaVersion string            `json:"schema_version"`
	Findings      []validateFinding `json:"findings"`
	ErrorCount    int               `json:"error_count"`
	WarningCount  int               `json:"warning_count"`
}

type repairChange struct {
	Field  string `json:"field"`
	Action string `json:"action"`
	From   string `json:"from"`
	To     string `json:"to"`
}

type repairReport struct {
	SchemaVersion string         `json:"schema_version"`
	Target        string         `json:"target"`
	DryRun        bool           `json:"dry_run"`
	CreatedMarker bool           `json:"created_marker"`
	Changed       bool           `json:"changed"`
	Changes       []repairChange `json:"changes"`
	Workitem      struct {
		ID            string `json:"id"`
		Ref           string `json:"ref"`
		Type          string `json:"type"`
		Title         string `json:"title"`
		RelativePath  string `json:"relative_path"`
		MarkerVersion string `json:"marker_version"`
	} `json:"workitem"`
}

func parseValidateReport(t *testing.T, raw string) validateReport {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var r validateReport
	require.NoError(t, dec.Decode(&r), "validate --json must produce valid JSON, got: %s", raw)
	return r
}

func validateCodeCount(report validateReport, code string) int {
	n := 0
	for _, f := range report.Findings {
		if f.Code == code {
			n++
		}
	}
	return n
}

func targetHasCode(report validateReport, target, code string) bool {
	for _, f := range report.Findings {
		if f.Target == target && f.Code == code {
			return true
		}
	}
	return false
}

// seedValidateFixture creates a campaign with a representative mix of workflow
// directories: a design dir with no marker, a design dir with a legacy marker
// (old schema, mismatched type, no ref), a dungeon dir, and an unmarked custom
// type dir. Returns nothing; callers assert against the fixed paths.
func seedValidateFixture(t *testing.T, tc *TestContainer, dir string) {
	t.Helper()
	initWorkflowCampaign(t, tc, dir)

	require.NoError(t, tc.WriteFile(
		dir+"/workflow/design/legacy-nomarker/README.md",
		"# Legacy Design Title\n\nbody\n"))

	require.NoError(t, tc.WriteFile(
		dir+"/workflow/design/legacy-marker/.workitem",
		"version: v1alpha5\nkind: workitem\nid: design-legacy-marker-2026-05-25\ntype: feature\ntitle: Legacy\n"))

	require.NoError(t, tc.WriteFile(
		dir+"/workflow/design/dungeon/ignored/README.md", "# Ignored\n"))

	_, _, err := tc.ExecCommand("mkdir", "-p", dir+"/workflow/feature/unmarked")
	require.NoError(t, err)
}

func TestIntegration_WorkitemValidate_ReportsFindings(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-validate-report"
	seedValidateFixture(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workitem", "validate", "--json")
	require.Error(t, err, "validate must exit non-zero when error findings exist: %s", out)
	report := parseValidateReport(t, out)

	assert.Equal(t, "workitem-validate/v1alpha1", report.SchemaVersion)
	assert.True(t, targetHasCode(report, "workflow/design/legacy-nomarker", "workitem.marker.missing"),
		"missing-marker design dir must be flagged:\n%s", out)
	assert.True(t, targetHasCode(report, "workflow/design/legacy-marker", "workitem.type.mismatch"),
		"type/path mismatch must be flagged:\n%s", out)
	assert.True(t, targetHasCode(report, "workflow/design/legacy-marker", "workitem.ref.missing"),
		"missing ref must be flagged:\n%s", out)
	assert.True(t, targetHasCode(report, "workflow/design/legacy-marker", "workitem.schema.outdated"),
		"legacy schema must be flagged:\n%s", out)

	assert.NotContains(t, out, "workflow/design/dungeon",
		"dungeon directories must be ignored:\n%s", out)
	assert.NotContains(t, out, "workflow/feature/unmarked",
		"unmarked custom-type dirs are not work items and must be ignored:\n%s", out)

	for _, f := range report.Findings {
		assert.Equal(t, "camp workitem repair "+f.Target, f.RepairCommand,
			"every finding must carry the exact repair command")
	}
}

func TestIntegration_WorkitemRepair_CreatesMarkerFromH1(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-repair-create"
	seedValidateFixture(t, tc, dir)

	const target = "workflow/design/legacy-nomarker"

	dryOut, err := tc.RunCampInDir(dir, "workitem", "repair", target, "--dry-run")
	require.NoError(t, err, "dry-run repair: %s", dryOut)
	assert.Contains(t, dryOut, "would create marker for "+target)
	exists, err := tc.CheckFileExists(dir + "/" + target + "/.workitem")
	require.NoError(t, err)
	assert.False(t, exists, "dry-run must not write the marker")

	out, err := tc.RunCampInDir(dir, "workitem", "repair", target, "--json")
	require.NoError(t, err, "repair --json: %s", out)
	var rep repairReport
	require.NoError(t, json.Unmarshal([]byte(out), &rep), "raw=%s", out)
	assert.Equal(t, "workitem-repair/v1alpha1", rep.SchemaVersion)
	assert.True(t, rep.CreatedMarker)
	assert.True(t, rep.Changed)
	assert.Equal(t, "design", rep.Workitem.Type)
	assert.Equal(t, "Legacy Design Title", rep.Workitem.Title, "title must come from the README H1")
	assert.Regexp(t, `^WI-[0-9a-f]{6}$`, rep.Workitem.Ref)
	assert.Equal(t, "v1alpha7", rep.Workitem.MarkerVersion)

	marker, err := tc.ReadFile(dir + "/" + target + "/.workitem")
	require.NoError(t, err)
	assert.Contains(t, marker, "version: v1alpha7")
	assert.Contains(t, marker, "type: design")
	assert.Contains(t, marker, "title: Legacy Design Title")

	readme, err := tc.ReadFile(dir + "/" + target + "/README.md")
	require.NoError(t, err)
	assert.Equal(t, "# Legacy Design Title\n\nbody\n", readme,
		"repair must never touch document contents")
}

func TestIntegration_WorkitemRepair_UpgradesLegacyMarkerIdempotently(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-repair-upgrade"
	seedValidateFixture(t, tc, dir)

	const target = "workflow/design/legacy-marker"

	out, err := tc.RunCampInDir(dir, "workitem", "repair", target)
	require.NoError(t, err, "repair legacy marker: %s", out)
	assert.Contains(t, out, "repaired "+target)

	marker, err := tc.ReadFile(dir + "/" + target + "/.workitem")
	require.NoError(t, err)
	assert.Contains(t, marker, "version: v1alpha7", "schema upgraded")
	assert.Contains(t, marker, "type: design", "type aligned to path")
	assert.Contains(t, marker, "id: design-legacy-marker-2026-05-25", "existing id preserved")
	assert.Regexp(t, `ref: WI-[0-9a-f]{6}`, marker, "ref backfilled")

	// Idempotent: a second repair reports no changes and leaves the marker byte-identical.
	again, err := tc.RunCampInDir(dir, "workitem", "repair", target)
	require.NoError(t, err, "idempotent repair: %s", again)
	assert.Contains(t, again, "already valid; no changes")
	markerAgain, err := tc.ReadFile(dir + "/" + target + "/.workitem")
	require.NoError(t, err)
	assert.Equal(t, marker, markerAgain, "idempotent repair must not rewrite the marker")

	// After repairing every flagged dir, validate is clean.
	_, err = tc.RunCampInDir(dir, "workitem", "repair", "workflow/design/legacy-nomarker")
	require.NoError(t, err)
	validateOut, err := tc.RunCampInDir(dir, "workitem", "validate", "--json")
	require.NoError(t, err, "validate after repair must exit 0: %s", validateOut)
	report := parseValidateReport(t, validateOut)
	assert.Equal(t, 0, report.ErrorCount+report.WarningCount,
		"no findings should remain after repairing every dir:\n%s", validateOut)
}

func TestIntegration_WorkitemRepair_RefusesUnparseableMarker(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-repair-unparseable"
	initWorkflowCampaign(t, tc, dir)

	const target = "workflow/design/broken"
	require.NoError(t, tc.WriteFile(dir+"/"+target+"/.workitem",
		"version: v1alpha5\n[not yaml{{{\n"))

	out, err := tc.RunCampInDir(dir, "workitem", "repair", target)
	require.Error(t, err, "repair must refuse an unparseable marker: %s", out)
	assert.Contains(t, out, "cannot parse existing .workitem")

	report := parseValidateReport(t, mustValidateJSON(t, tc, dir, target))
	assert.Equal(t, 1, validateCodeCount(report, "workitem.marker.malformed"))
	assert.False(t, report.Findings[0].Repairable,
		"unparseable marker must be reported as non-repairable")
}

func mustValidateJSON(t *testing.T, tc *TestContainer, dir, target string) string {
	t.Helper()
	out, _ := tc.RunCampInDir(dir, "workitem", "validate", target, "--json")
	return out
}
