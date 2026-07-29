//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupGuardCampaign builds a campaign with ordinary content. Large fixtures
// are generated in-container per test and never committed to the repo.
func setupGuardCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()

	path := "/campaigns/" + name
	_, err := tc.InitCampaign(path, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p notes
		printf 'first note' > notes/one.md
		printf 'second note' > notes/two.md
	`, path))

	return path
}

// writeGuardConfig sets commit.guards.* in campaign.yaml. Sizes stay small so
// fixtures are cheap; the guard's logic does not care about magnitude.
func writeGuardConfig(t *testing.T, tc *TestContainer, campPath, block string) {
	t.Helper()
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		cat >> .campaign/campaign.yaml <<'YAML'
commit:
  guards:
%s
YAML
	`, campPath, block))
}

// stagedPaths returns the index contents after a staging operation.
func stagedPaths(t *testing.T, tc *TestContainer, campPath string) []string {
	t.Helper()
	out := tc.GitOutput(t, campPath, "diff", "--cached", "--name-only")
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

// Criterion 4: a repository with nothing over the limits stages exactly as it
// did before the guard existed, and says nothing extra.
func TestIntegration_GuardUnderLimitsIsInvisible(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-under-limits")

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "ordinary content")
	require.NoError(t, err, "output:\n%s", output)

	assert.NotContains(t, output, "artifact root")
	assert.NotContains(t, output, "over the size limit")
	assert.NotContains(t, output, "untracked files")

	committed := tc.GitOutput(t, campPath, "show", "--stat", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "notes/one.md")
	assert.Contains(t, committed, "notes/two.md")
}

// Criterion 1 (staging half): an over-threshold untracked file is kept out of
// the index while everything else goes in.
func TestIntegration_GuardExcludesOverThresholdFile(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-excludes")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		dd if=/dev/zero of=media/footage.mov bs=1024 count=3072 2>/dev/null
		printf 'script' > media/script.md
	`, campPath))

	_, err := tc.RunCampInDir(campPath, "commit", "-m", "add footage and script")
	require.NoError(t, err)

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "media/script.md",
		"small files beside the large one must still be committed")
	assert.NotContains(t, committed, "media/footage.mov",
		"the over-threshold file must be kept out of the commit")

	// The bytes are still on disk; the guard excludes, it does not delete.
	exists, err := tc.CheckFileExists(campPath + "/media/footage.mov")
	require.NoError(t, err)
	assert.True(t, exists, "the guard must never remove the user's file")
}

// Design doc 03 Change 3: an explicit `git add <path>` is an intent, so
// naming the file stages it regardless of size.
func TestIntegration_GuardIgnoresExplicitlyNamedFiles(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-explicit")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		dd if=/dev/zero of=media/footage.mov bs=1024 count=3072 2>/dev/null
		git add media/footage.mov
	`, campPath))

	staged := stagedPaths(t, tc, campPath)
	assert.Contains(t, staged, "media/footage.mov",
		"an explicitly named path is an intent the guard must not override")
}

// Criterion 12: a bulk refusal aborts before staging, leaving the index
// exactly as it was.
func TestIntegration_GuardBulkBlockLeavesIndexUntouched(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-bulk-block")
	writeGuardConfig(t, tc, campPath, "    max_added_files: 50\n    bulk: block")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p vendor_tree
		for i in $(seq 1 120); do printf 'x' > vendor_tree/f$i.js; done
	`, campPath))

	before := stagedPaths(t, tc, campPath)
	require.Empty(t, before, "index should start clean")

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "-m", "vendor tree")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "bulk block must fail the command; stdout:\n%s", stdout)
	assert.Contains(t, stdout+stderr, "vendor_tree")

	after := stagedPaths(t, tc, campPath)
	assert.Empty(t, after,
		"a bulk refusal must stage nothing at all; index held: %v", after)
}

// Criterion 13: bulk: off disables detection entirely.
func TestIntegration_GuardBulkOffDisablesDetection(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-bulk-off")
	writeGuardConfig(t, tc, campPath, "    max_added_files: 50\n    bulk: \"off\"")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p vendor_tree
		for i in $(seq 1 120); do printf 'x' > vendor_tree/f$i.js; done
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "vendor tree with guard off")
	require.NoError(t, err, "output:\n%s", output)

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "vendor_tree/f1.js",
		"with bulk off the tree must be committed normally")
}

// large_files: off disables the size guard while leaving bulk alone.
func TestIntegration_GuardLargeFilesOffStagesBigFile(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-large-off")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB\n    large_files: \"off\"")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		dd if=/dev/zero of=media/footage.mov bs=1024 count=3072 2>/dev/null
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "big file, guard off")
	require.NoError(t, err, "output:\n%s", output)

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "media/footage.mov",
		"with large_files off the file must be committed normally")
}

// The allowlist exempts a path permanently, in every mode.
func TestIntegration_GuardAllowlistExemptsPath(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-allowlist")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB\n    allow:\n      - \"releases/**\"")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p releases
		dd if=/dev/zero of=releases/camp-darwin bs=1024 count=3072 2>/dev/null
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "release binary")
	require.NoError(t, err, "output:\n%s", output)

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "releases/camp-darwin",
		"an allowlisted path must be staged despite its size")
}

// The campaign root always excludes submodule refs, so guard exclusions have
// to compose with an exclusion list the caller already built.
func TestIntegration_GuardComposesWithSubmoduleExclusions(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, "guard-submodule-compose")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		dd if=/dev/zero of=media/footage.mov bs=1024 count=3072 2>/dev/null
		printf 'root note' > root-note.md
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "root content beside a big file")
	require.NoError(t, err, "output:\n%s", output)

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "root-note.md",
		"ordinary root content must commit with both exclusion sets active")
	assert.NotContains(t, committed, "media/footage.mov",
		"the guard exclusion must survive composition with submodule exclusions")
}
