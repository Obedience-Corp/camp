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

type doctorJSONDoc struct {
	SchemaVersion string `json:"schema_version"`
	Success       bool   `json:"success"`
	Passed        int    `json:"passed"`
	Warned        int    `json:"warned"`
	Failed        int    `json:"failed"`
	Issues        []struct {
		Severity    any            `json:"severity"`
		CheckID     string         `json:"check_id"`
		Description string         `json:"description"`
		AutoFixable bool           `json:"auto_fixable"`
		Details     map[string]any `json:"details"`
	} `json:"issues"`
}

// declareRoots writes artifacts.yaml directly. Several rows describe states
// camp itself refuses to create, so they can only be reached by hand-editing
// the committed declaration file, which is exactly why the check exists.
func declareRoots(t *testing.T, tc *TestContainer, campPath string, roots ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("version: 1\nroots:\n")
	for _, r := range roots {
		fmt.Fprintf(&b, "  - path: %s\n", r)
	}
	require.NoError(t, tc.WriteFile(campPath+"/.campaign/artifacts.yaml", b.String()))
}

// runDoctorArtifacts runs the check and returns its findings by reason.
func runDoctorArtifacts(t *testing.T, tc *TestContainer, campPath string, extra ...string) (string, map[string]string) {
	t.Helper()
	args := append([]string{"doctor", "-c", "artifacts", "--json"}, extra...)
	stdout, _, _, err := tc.RunCampSplitInDir(campPath, args...)
	require.NoError(t, err)

	var doc doctorJSONDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc),
		"doctor --json must emit one document; got:\n%s", stdout)

	byReason := make(map[string]string)
	for _, issue := range doc.Issues {
		if issue.CheckID != "artifacts" {
			continue
		}
		if reason, ok := issue.Details["reason"].(string); ok {
			byReason[reason] = issue.Description
		}
	}
	return stdout, byReason
}

// Criterion 25: a clean root with no ignore rule is an error, and --fix
// appends the rule exactly once.
func TestIntegration_DoctorArtifactsCleanRootNotIgnored(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "doctor-clean-root")

	tc.Shell(t, fmt.Sprintf(`cd %s && mkdir -p media/renders && printf 'x' > media/renders/frame.exr`, campPath))
	declareRoots(t, tc, campPath, "media/renders")

	_, byReason := runDoctorArtifacts(t, tc, campPath)
	require.Contains(t, byReason, "clean_root_not_gitignored")
	assert.Contains(t, byReason["clean_root_not_gitignored"], "media/renders")

	// --fix appends the rule.
	_, _, _, err := tc.RunCampSplitInDir(campPath, "doctor", "-c", "artifacts", "--fix")
	require.NoError(t, err)

	gitignore := readGitignore(t, tc, campPath)
	require.Equal(t, 1, countRuleLines(gitignore, "media/renders/"),
		"the rule must be appended exactly once; got:\n%s", gitignore)

	// Idempotent: a second --fix does not append again, and the finding is gone.
	_, _, _, err = tc.RunCampSplitInDir(campPath, "doctor", "-c", "artifacts", "--fix")
	require.NoError(t, err)
	gitignore = readGitignore(t, tc, campPath)
	assert.Equal(t, 1, countRuleLines(gitignore, "media/renders/"),
		"re-running --fix must not duplicate the rule; got:\n%s", gitignore)

	_, byReason = runDoctorArtifacts(t, tc, campPath)
	assert.NotContains(t, byReason, "clean_root_not_gitignored",
		"the finding must clear once the rule exists")
}

// A mixed root is a supported state, so it must never be a finding. Flagging
// it would train users to ignore this check.
func TestIntegration_DoctorArtifactsMixedRootIsNotAFinding(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupMixedRootCampaign(t, tc, "doctor-mixed-root")
	declareRoots(t, tc, campPath, "videos/my-video")

	stdout, byReason := runDoctorArtifacts(t, tc, campPath)

	assert.NotContains(t, byReason, "clean_root_not_gitignored",
		"a root holding tracked files must not be reported as an un-ignored clean root")
	assert.NotContains(t, strings.ToLower(stdout), "mixed root",
		"mixed is informational, never a finding")
}

// Criterion 26: --fix never untracks a file. A root that has tracked content
// must keep it, whatever else the fixer does.
func TestIntegration_DoctorArtifactsFixNeverUntracks(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupMixedRootCampaign(t, tc, "doctor-fix-no-untrack")
	declareRoots(t, tc, campPath, "videos/my-video")

	before := strings.Fields(tc.GitOutput(t, campPath, "ls-files", "--", "videos/my-video"))
	require.NotEmpty(t, before, "fixture must have tracked content under the root")

	_, _, _, err := tc.RunCampSplitInDir(campPath, "doctor", "-c", "artifacts", "--fix")
	require.NoError(t, err)

	after := strings.Fields(tc.GitOutput(t, campPath, "ls-files", "--", "videos/my-video"))
	assert.Equal(t, before, after, "--fix must never untrack a file")

	gitignore := readGitignore(t, tc, campPath)
	assert.Equal(t, 0, countRuleLines(gitignore, "videos/my-video/"),
		"a mixed root must never be gitignored by --fix; got:\n%s", gitignore)
}

// A declared root absent locally is a warning, not an error: it may live on
// another machine, which is the reason declarations are committed at all.
func TestIntegration_DoctorArtifactsMissingRootIsWarning(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "doctor-missing-root")
	declareRoots(t, tc, campPath, "not/here/yet")

	stdout, byReason := runDoctorArtifacts(t, tc, campPath)
	require.Contains(t, byReason, "root_missing_locally")
	assert.Contains(t, byReason["root_missing_locally"], "another machine")

	var doc doctorJSONDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	assert.True(t, doc.Success,
		"a missing root is a warning, so the check must still succeed")
	assert.Equal(t, 0, doc.Failed, "a warning must not be counted as a failure")
}

// The remaining rows describe states camp refuses to create; a hand-edited
// artifacts.yaml is the only way to reach them.
func TestIntegration_DoctorArtifactsHandEditedBadStates(t *testing.T) {
	t.Run("nested inside another declared root", func(t *testing.T) {
		tc := GetSharedContainer(t)
		campPath := setupGuardCampaign(t, tc, "doctor-nested-root")
		tc.Shell(t, fmt.Sprintf(`cd %s && mkdir -p videos/inner && printf 'x' > videos/inner/a.bin`, campPath))
		declareRoots(t, tc, campPath, "videos", "videos/inner")

		_, byReason := runDoctorArtifacts(t, tc, campPath)
		require.Contains(t, byReason, "root_nested_in_declared_root")
		assert.Contains(t, byReason["root_nested_in_declared_root"], "videos")
	})

	t.Run("crosses a submodule boundary", func(t *testing.T) {
		tc := GetSharedContainer(t)
		campPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "doctor-submodule-root")
		declareRoots(t, tc, campPath, "projects/test-project/media")

		_, byReason := runDoctorArtifacts(t, tc, campPath)
		require.Contains(t, byReason, "root_crosses_submodule_boundary")
		desc := byReason["root_crosses_submodule_boundary"]
		assert.Contains(t, desc, "never reach the submodule's remote")
		assert.Contains(t, desc, "hand-written",
			"the finding should say camp cannot create this state")
	})

	t.Run("escapes the campaign", func(t *testing.T) {
		tc := GetSharedContainer(t)
		campPath := setupGuardCampaign(t, tc, "doctor-escaping-root")
		declareRoots(t, tc, campPath, "../outside")

		_, byReason := runDoctorArtifacts(t, tc, campPath)
		require.Contains(t, byReason, "root_escapes_campaign")
	})

	t.Run("also tracked by DVC", func(t *testing.T) {
		tc := GetSharedContainer(t)
		campPath := setupGuardCampaign(t, tc, "doctor-dvc-root")
		tc.Shell(t, fmt.Sprintf(`
			cd %s
			mkdir -p datasets
			printf 'x' > datasets/train.bin
			printf 'outs:\n- path: datasets\n' > datasets.dvc
		`, campPath))
		declareRoots(t, tc, campPath, "datasets")

		_, byReason := runDoctorArtifacts(t, tc, campPath)
		require.Contains(t, byReason, "root_also_dvc_tracked")
		assert.Contains(t, byReason["root_also_dvc_tracked"], "DVC")
	})
}

// Criterion 28: the check joins doctor --json without breaking its envelope,
// and every finding carries its row's disposition.
func TestIntegration_DoctorArtifactsJSONContract(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "doctor-artifacts-json")
	tc.Shell(t, fmt.Sprintf(`cd %s && mkdir -p media && printf 'x' > media/a.bin`, campPath))
	declareRoots(t, tc, campPath, "media")

	stdout, _ := runDoctorArtifacts(t, tc, campPath)

	var doc doctorJSONDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	assert.Equal(t, "doctor/v1alpha1", doc.SchemaVersion,
		"the existing doctor envelope must be unchanged")

	var found bool
	for _, issue := range doc.Issues {
		if issue.CheckID != "artifacts" {
			continue
		}
		found = true
		assert.NotEmpty(t, issue.Details["reason"], "every finding must name its row")
		assert.NotEmpty(t, issue.Details["root"], "every finding must name its root")
	}
	assert.True(t, found, "the artifacts check must appear in doctor --json")

	// The document must satisfy a real JSON consumer, not just our unmarshaler.
	out := tc.Shell(t, fmt.Sprintf(
		`cd %s && /camp doctor -c artifacts --json 2>/dev/null | jq -r '.issues[] | select(.check_id=="artifacts") | .details.reason'`,
		campPath))
	assert.Contains(t, out, "clean_root_not_gitignored")
}
