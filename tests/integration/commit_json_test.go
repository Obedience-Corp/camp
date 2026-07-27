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

type commitJSONDoc struct {
	SchemaVersion string `json:"schema_version"`
	OK            bool   `json:"ok"`
	Repo          string `json:"repo"`
	Commit        string `json:"commit"`
	Staged        int    `json:"staged"`
	Excluded      []struct {
		Path         string `json:"path"`
		Size         int64  `json:"size"`
		Reason       string `json:"reason"`
		ArtifactRoot string `json:"artifact_root"`
	} `json:"excluded"`
	ArtifactRootsDeclared []string `json:"artifact_roots_declared"`
}

// runCommitJSON runs a commit with --json and returns raw stdout plus the
// parsed document. stdout must be the document and nothing else.
func runCommitJSON(t *testing.T, tc *TestContainer, dir string, args ...string) (string, commitJSONDoc) {
	t.Helper()
	full := append([]string{"commit", "--json"}, args...)
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(dir, full...)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	var doc commitJSONDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc),
		"stdout must be exactly one JSON document; got:\n%s", stdout)
	return stdout, doc
}

// Criterion 15c: --json is synchronous by contract. The document always
// carries a real hash, and that hash is HEAD.
func TestIntegration_CommitJSONCarriesRealHash(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "commit-json-hash")

	stdout, doc := runCommitJSON(t, tc, campPath, "-m", "ordinary content")

	assert.Equal(t, "commit/v1alpha1", doc.SchemaVersion)
	assert.True(t, doc.OK)
	require.NotEmpty(t, doc.Commit, "the document must carry a real commit hash, never a promise")
	assert.Len(t, doc.Commit, 40, "machine callers get the full hash, not an abbreviation")

	head := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))
	assert.Equal(t, head, doc.Commit, "the reported hash must be HEAD")
	assert.Greater(t, doc.Staged, 0)

	// Nothing but the document on stdout.
	assert.True(t, strings.HasPrefix(strings.TrimSpace(stdout), "{"),
		"stdout must start with the document; got:\n%s", stdout)
}

// Criterion 15e: empty collections marshal to [], never null. Asserted on raw
// bytes, because unmarshaling hides the difference and that is exactly how
// this class of bug reaches consumers.
func TestIntegration_CommitJSONEmptyArraysAreNotNull(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "commit-json-empty")

	stdout, doc := runCommitJSON(t, tc, campPath, "-m", "nothing excluded")

	assert.Contains(t, stdout, `"excluded": []`,
		"excluded must serialize as an empty array; got:\n%s", stdout)
	assert.Contains(t, stdout, `"artifact_roots_declared": []`,
		"artifact_roots_declared must serialize as an empty array; got:\n%s", stdout)
	assert.NotContains(t, stdout, "null",
		"no field may serialize as null; got:\n%s", stdout)

	assert.Empty(t, doc.Excluded)
	assert.Empty(t, doc.ArtifactRootsDeclared)
}

// The load-bearing case: a machine caller can see that a file it wrote never
// entered the commit, and which root now owns it.
func TestIntegration_CommitJSONReportsExcludedFile(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupMixedRootCampaign(t, tc, "commit-json-excluded")

	_, doc := runCommitJSON(t, tc, campPath, "-m", "rough cut notes")

	require.Len(t, doc.Excluded, 1, "the excluded footage must appear in the document")
	ex := doc.Excluded[0]
	assert.Equal(t, "videos/my-video/footage.mp4", ex.Path)
	assert.Equal(t, int64(3145728), ex.Size)
	assert.Equal(t, "size_guard", ex.Reason)
	assert.Equal(t, "videos/my-video", ex.ArtifactRoot)

	assert.Equal(t, []string{"videos/my-video"}, doc.ArtifactRootsDeclared)
}

// A refusal reports its own reason and declares no root.
func TestIntegration_CommitJSONReportsRefusalReason(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "commit-json-refusal")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		dd if=/dev/zero of=render.mov bs=1024 count=3072 2>/dev/null
		printf 'ordinary' > ordinary.md
	`, campPath))

	_, doc := runCommitJSON(t, tc, campPath, "-m", "root-level media")

	require.Len(t, doc.Excluded, 1)
	assert.Equal(t, "render.mov", doc.Excluded[0].Path)
	assert.Equal(t, "needs_root_decision", doc.Excluded[0].Reason)
	assert.Empty(t, doc.Excluded[0].ArtifactRoot,
		"a refusal declares nothing, so it names no root")
	assert.Empty(t, doc.ArtifactRootsDeclared)
}

// The document must be valid input to a real JSON consumer, not merely
// parseable by our own unmarshaler.
func TestIntegration_CommitJSONPipesToJq(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupMixedRootCampaign(t, tc, "commit-json-jq")

	out := tc.Shell(t, fmt.Sprintf(
		`cd %s && /camp commit --json -m "jq check" 2>/dev/null | jq -r '.excluded[0].reason, .artifact_roots_declared[0]'`,
		campPath))

	assert.Contains(t, out, "size_guard")
	assert.Contains(t, out, "videos/my-video")
}

// Criterion 37c2: the human path is unchanged by the existence of --json.
func TestIntegration_CommitHumanPathUnchangedWithoutJSON(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "commit-human-baseline")

	stdout, _, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "-m", "human path")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	assert.Contains(t, stdout, "Changes to be committed:")
	assert.Contains(t, stdout, "Changes committed successfully")
	assert.NotContains(t, stdout, "schema_version",
		"the human path must not emit machine output")
}
