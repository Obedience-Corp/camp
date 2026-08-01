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

// The gap these cover: camp's size guard lives at the staging chokepoint, so a
// commit that stages nothing never reaches it. A user who ran `git add` by hand
// and then committed with --all=false could put a 13 MB PNG into git without
// camp saying a word.
//
// What the fix is allowed to do is report. The commit must land byte-for-byte
// as it would have without the check, so every case below asserts the file is
// committed as well as reported.

// stagePreStagedLargeFile builds the reported scenario: a file over the
// threshold placed in the index by raw git add, with an ordinary file beside it
// so the commit has more than one thing in it.
func stagePreStagedLargeFile(t *testing.T, tc *TestContainer, campPath string) {
	t.Helper()
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		dd if=/dev/zero of=media/diagram.png bs=1024 count=3072 2>/dev/null
		printf 'a caption' > media/caption.md
		git add media/diagram.png media/caption.md
	`, campPath))
}

// The reported incident, end to end: pre-staged over-threshold content is
// reported, and still committed.
func TestIntegration_ScopedCommitReportsPreStagedGrowth(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "scoped-commit-report")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")
	stagePreStagedLargeFile(t, tc, campPath)

	output, err := tc.RunCampInDir(campPath, "commit", "--all=false", "-m", "hand-staged diagram")
	require.NoError(t, err, "output:\n%s", output)

	// The staging path's copy, verbatim. A user must not have to notice that
	// the wording changes with who did the staging.
	assert.Contains(t, output, "media/diagram.png is tracked and now")
	assert.Contains(t, output, "Tracked files are always committed")
	assert.Contains(t, output, "git rm --cached media/diagram.png")
	assert.Contains(t, output, "camp artifacts add media")
	assert.Contains(t, output, "commit.guards.allow")

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", latestUserCommit(t, tc, campPath))
	assert.Contains(t, committed, "media/diagram.png",
		"the report must not change what is committed")
	assert.Contains(t, committed, "media/caption.md",
		"everything else in the index commits as usual")

	// Report-only means report only. Nothing may be declared, excluded, or
	// gitignored on the user's behalf.
	exists, err := tc.CheckFileExists(campPath + "/.campaign/artifacts.yaml")
	require.NoError(t, err)
	if exists {
		declared, readErr := tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
		require.NoError(t, readErr)
		assert.NotContains(t, declared, "media",
			"a scoped commit must never declare an artifact root")
	}
	assert.NotContains(t, readGitignore(t, tc, campPath), "media/",
		"a scoped commit must never write an ignore rule")
}

// The other half of the contract: an ordinary scoped commit stays quiet. A
// report on every commit would be noise, and noise is what makes a real report
// easy to miss.
func TestIntegration_ScopedCommitStaysSilentUnderThreshold(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "scoped-commit-quiet")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		printf 'well under the limit' > media/note.md
		git add media/note.md
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "--all=false", "-m", "small note")
	require.NoError(t, err, "output:\n%s", output)

	assert.NotContains(t, output, "is tracked and now",
		"an under-threshold scoped commit must say nothing about size")
	assert.NotContains(t, output, "Could not run the size check",
		"the check must have run rather than degrading")

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", latestUserCommit(t, tc, campPath))
	assert.Contains(t, committed, "media/note.md")
}

// --commit-large is the user having already answered. It suppresses detection
// on the staging path; it has to mean the same thing here, or the flag would
// silence camp in one command and not the other.
func TestIntegration_ScopedCommitLargeSuppressesTheReport(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "scoped-commit-large")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")
	stagePreStagedLargeFile(t, tc, campPath)

	output, err := tc.RunCampInDir(campPath,
		"commit", "--all=false", "--commit-large", "-m", "yes, it belongs in git")
	require.NoError(t, err, "output:\n%s", output)

	assert.NotContains(t, output, "is tracked and now")

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", latestUserCommit(t, tc, campPath))
	assert.Contains(t, committed, "media/diagram.png")
}

// The allowlist is the one line that makes a legitimate large file permanent.
// If it did not reach this pass, a user who had already allowed a path would be
// told about it on every scoped commit, with no way to stop it.
func TestIntegration_ScopedCommitRespectsAllowlist(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "scoped-commit-allow")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB\n    allow:\n      - \"*.png\"")
	stagePreStagedLargeFile(t, tc, campPath)

	output, err := tc.RunCampInDir(campPath, "commit", "--all=false", "-m", "allowed diagram")
	require.NoError(t, err, "output:\n%s", output)

	assert.NotContains(t, output, "is tracked and now",
		"an allowlisted path must not be reported")

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", latestUserCommit(t, tc, campPath))
	assert.Contains(t, committed, "media/diagram.png")
}

// The report is human output, so under --json it belongs on stderr with the
// rest of it. stdout carries one document and nothing else, which is the whole
// reason a machine caller can parse it without a filter.
func TestIntegration_ScopedCommitJSONStdoutStaysPure(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "scoped-commit-json")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")
	stagePreStagedLargeFile(t, tc, campPath)

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath,
		"commit", "--json", "--all=false", "-m", "hand-staged diagram")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	var doc commitJSONDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc),
		"stdout must be exactly one JSON document; got:\n%s", stdout)
	assert.True(t, doc.OK)
	assert.NotContains(t, stdout, "is tracked and now",
		"the report must not leak into the document stream")

	assert.Contains(t, stderr, "media/diagram.png is tracked and now",
		"a person watching a scripted run still has to see it; stderr:\n%s", stderr)

	// Nothing was kept out, so the document says nothing was.
	assert.Empty(t, doc.Excluded)
	assert.Empty(t, doc.ArtifactRootsDeclared)

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", doc.Commit)
	assert.Contains(t, committed, "media/diagram.png")
}

// A submodule gitlink is staged at the campaign root routinely, and its object
// id names a commit that lives in the submodule rather than the parent. Asking
// git to size it there would be meaningless; the pass has to skip it without
// falling over, and without losing the ordinary files beside it.
func TestIntegration_ScopedCommitSkipsStagedSubmoduleRefs(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "scoped-commit-gitlink")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	// A gitlink written straight into the index, pointing at an object the
	// campaign repository does not have. This is the shape `git add` leaves
	// behind for a submodule, without the cost of building one.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		dd if=/dev/zero of=media/diagram.png bs=1024 count=3072 2>/dev/null
		git add media/diagram.png
		git update-index --add --cacheinfo 160000,0000000000000000000000000000000000000001,vendor/thing
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "--all=false", "-m", "diagram plus a gitlink")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "media/diagram.png is tracked and now",
		"a gitlink in the index must not cost the report the files beside it")
	assert.NotContains(t, output, "Could not run the size check",
		"an unreachable gitlink object must not fail the check")
	assert.NotContains(t, output, "vendor/thing is tracked and now",
		"a gitlink has no blob size to report")
}

// Everything above runs at the campaign root. A project has its own threshold
// and its own remedy, and it is a separate command; the report has to reach it
// too or the gap simply moves.
func TestIntegration_ScopedProjectCommitReportsPreStagedGrowth(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupFreshCampaignWithSubmodule(t, tc, "scoped-project-report")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		cat >> .campaign/campaign.yaml <<'YAML'
commit:
  guards:
    max_project_file_size: 1MiB
YAML
	`, campPath))

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p testdata
		dd if=/dev/zero of=testdata/corpus.tar bs=1024 count=3072 2>/dev/null
		git add testdata/corpus.tar
	`, projPath))

	output, err := tc.RunCampInDir(projPath,
		"project", "commit", "--all=false", "--no-sync", "-m", "hand-staged fixture")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "testdata/corpus.tar is tracked and now")
	assert.Contains(t, output, "Tracked files are always committed")

	committed := tc.GitOutput(t, projPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "testdata/corpus.tar",
		"the report must not change what a project commit contains")
}

// A wide index is the case that would expose a per-file design: 400 staged
// paths cost two git processes here, not 400. The assertion is that the commit
// is still whole and still silent, since nothing in it crosses the threshold.
func TestIntegration_ScopedCommitHandlesAWideIndex(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "scoped-commit-cost")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p bulk
		for i in $(seq 1 400); do printf 'entry %%s' "$i" > bulk/file-$i.md; done
		git add bulk
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "--all=false", "-m", "four hundred small files")
	require.NoError(t, err, "output:\n%s", output)
	assert.NotContains(t, output, "is tracked and now",
		"nothing here crosses the threshold, so nothing may be reported")

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", latestUserCommit(t, tc, campPath))
	assert.Contains(t, committed, "bulk/file-1.md")
	assert.Contains(t, committed, "bulk/file-400.md",
		"a wide scoped commit still commits everything it was given")
}
