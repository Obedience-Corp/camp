//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Criterion 14: a bulk directory refuses the whole commit, stages nothing, and
// prints the three ways forward with the detected prefix interpolated.
func TestIntegration_GuardBulkRefusalRendersMenu(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-bulk-menu")
	writeGuardConfig(t, tc, campPath, "    max_added_files: 50")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p vendor_tree
		for i in $(seq 1 120); do printf 'x' > vendor_tree/f$i.js; done
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "-m", "vendor")
	require.NoError(t, err)
	require.NotEqual(t, 0, exitCode)
	combined := stdout + stderr

	assert.Contains(t, combined, "untracked files")
	assert.Contains(t, combined, "would be committed")
	assert.Contains(t, combined, "vendor_tree")
	assert.Contains(t, combined, "Nothing was staged")

	// All three ways forward, with the detected prefix interpolated. No
	// directory name is hardcoded: criterion 36 means shape decides, never a
	// name camp was taught to recognize.
	assert.Contains(t, combined, "echo 'vendor_tree/' >> .gitignore")
	assert.Contains(t, combined, "camp artifacts add vendor_tree")
	assert.Contains(t, combined, "--commit-large")
	// Criterion 37d: the message announcing the behavior carries its off switch.
	assert.Contains(t, combined, "camp settings set local.commit.guards.bulk off")

	assert.Empty(t, stagedPaths(t, tc, campPath), "a refusal must stage nothing")
}

// Criterion 14b: many medium files are not a bulk trigger. There is no
// total-bytes leg, precisely because its only unique catch was this case.
func TestIntegration_GuardPhotoBatchDoesNotBlock(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-photo-batch")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 50MiB\n    max_added_files: 50")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p photos
		for i in $(seq 1 6); do dd if=/dev/zero of=photos/p$i.jpg bs=1024 count=9216 2>/dev/null; done
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "six photos")
	require.NoError(t, err, "six 9 MB photos must not block; output:\n%s", output)

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "photos/p1.jpg")
}

// Criterion 14c: the per-file guard runs first, so an auto-handled large file
// does not also swell the bulk batch back over the trigger.
func TestIntegration_GuardLargeFileDoesNotTripBulk(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-order")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB\n    max_added_files: 100")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		dd if=/dev/zero of=media/footage.mov bs=1024 count=3072 2>/dev/null
		for i in $(seq 1 99); do printf 'x' > media/small$i.txt; done
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "one big plus ninety-nine small")
	require.NoError(t, err, "the big file must be taken by the per-file guard, not counted toward bulk; output:\n%s", output)

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "media/small1.txt")
	assert.NotContains(t, committed, "media/footage.mov")
}

// Criterion 12: large_files: block refuses, stages nothing, and prints the
// exact camp artifacts add command for the real directory.
func TestIntegration_GuardLargeFilesBlockMode(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-block-mode")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB\n    large_files: block")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		dd if=/dev/zero of=media/footage.mov bs=1024 count=3072 2>/dev/null
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "-m", "blocked")
	require.NoError(t, err)
	require.NotEqual(t, 0, exitCode)
	combined := stdout + stderr

	assert.Contains(t, combined, "media/footage.mov")
	assert.Contains(t, combined, "Nothing was staged")
	assert.Contains(t, combined, "camp artifacts add media")
	assert.Contains(t, combined, "--commit-large")
	assert.Contains(t, combined, "camp settings set local.commit.guards.large_files auto")

	assert.Empty(t, stagedPaths(t, tc, campPath), "block mode must leave the index untouched")
}

// Criterion 3: --commit-large forces the file in and declares nothing.
func TestIntegration_GuardCommitLargeForcesInclusion(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-commit-large")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		dd if=/dev/zero of=media/footage.mov bs=1024 count=3072 2>/dev/null
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "--commit-large", "-m", "keep the binary")
	require.NoError(t, err, "output:\n%s", output)

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "media/footage.mov",
		"--commit-large must force the file into the commit")

	exists, err := tc.CheckFileExists(campPath + "/.campaign/artifacts.yaml")
	require.NoError(t, err)
	if exists {
		declared, err := tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
		require.NoError(t, err)
		assert.NotContains(t, declared, "media",
			"--commit-large declares nothing; it is an override, not a different handling")
	}
	assert.NotContains(t, output, "is now an artifact root")
}

// --commit-large also overrides a bulk refusal, which is what the bulk menu
// offers as the third way forward.
func TestIntegration_GuardCommitLargeOverridesBulk(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-large-bulk")
	writeGuardConfig(t, tc, campPath, "    max_added_files: 50")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p vendor_tree
		for i in $(seq 1 120); do printf 'x' > vendor_tree/f$i.js; done
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "--commit-large", "-m", "vendor anyway")
	require.NoError(t, err, "the bulk menu offers --commit-large, so it must work; output:\n%s", output)

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "vendor_tree/f1.js")
}

// Criterion 5: an allowlisted file commits normally, with no declaration and
// no report. An exemption the user configured is not news.
func TestIntegration_GuardAllowlistIsSilent(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-allow-silent")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB\n    allow:\n      - \"releases/**\"")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p releases
		dd if=/dev/zero of=releases/camp-darwin bs=1024 count=3072 2>/dev/null
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "release binary")
	require.NoError(t, err, "output:\n%s", output)

	assert.NotContains(t, output, "artifact root")
	assert.NotContains(t, output, "kept out of git")

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "releases/camp-darwin")
}

// Criterion 35: a per-project override applies to that project only.
func TestIntegration_GuardProjectOverrideDoesNotAffectSiblings(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupFreshCampaignWithSubmodule(t, tc, "guard-project-override")

	// alpha raises its own limit; the submodule project keeps the default.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		cat >> .campaign/campaign.yaml <<'YAML'
commit:
  guards:
    max_project_file_size: 1MiB
    projects:
      test-project:
        max_file_size: 100MiB
YAML
	`, campPath))

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p bigdir
		dd if=/dev/zero of=bigdir/blob.bin bs=1024 count=3072 2>/dev/null
	`, projPath))

	output, err := tc.RunCampInDir(projPath, "project", "commit", "-m", "override applies here")
	require.NoError(t, err, "output:\n%s", output)

	committed := tc.GitOutput(t, projPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "bigdir/blob.bin",
		"the project's own 100MiB override must let a 3 MB file through")
	assert.NotContains(t, output, "was left out of this commit")
}

// Criterion 34: camp settings set persists the threshold, and later commits
// simply honor it with no flags and no output.
func TestIntegration_GuardSettingsRoundTrip(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-settings")

	out, err := tc.RunCampInDir(campPath, "settings", "set", "local.commit.guards.max_file_size", "100MiB")
	require.NoError(t, err, "output:\n%s", out)

	got, err := tc.RunCampInDir(campPath, "settings", "get", "local.commit.guards.max_file_size")
	require.NoError(t, err)
	assert.Contains(t, got, "100MiB", "the setting must round-trip")

	yaml, err := tc.ReadFile(campPath + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.Contains(t, yaml, "100MiB", "the setting must persist to campaign.yaml")

	// A 60 MB file now commits with no flags and no guard output.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		dd if=/dev/zero of=media/big.bin bs=1024 count=61440 2>/dev/null
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "under the raised limit")
	require.NoError(t, err, "output:\n%s", output)
	assert.NotContains(t, output, "artifact root")

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "media/big.bin")
}

// Invalid guard settings are rejected at set time, using the same parser the
// guard uses, so a value accepted here can never be rejected at commit time.
func TestIntegration_GuardSettingsRejectInvalidValues(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-settings-invalid")

	cases := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "unparseable size", key: "local.commit.guards.max_file_size", value: "ten megs", want: "10MiB"},
		{name: "unknown large_files mode", key: "local.commit.guards.large_files", value: "warn", want: "auto, block, or off"},
		{name: "bulk rejects auto", key: "local.commit.guards.bulk", value: "auto", want: "block or off"},
		{name: "non-numeric count", key: "local.commit.guards.max_added_files", value: "many", want: "non-negative"},
	}

	for _, tc2 := range cases {
		t.Run(tc2.name, func(t *testing.T) {
			stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "settings", "set", tc2.key, tc2.value)
			require.NoError(t, err)
			assert.NotEqual(t, 0, exitCode, "stdout:\n%s", stdout)
			assert.Contains(t, stdout+stderr, tc2.want)
		})
	}
}

// A negative count is rejected too, but it has to be written after a "--"
// separator: cobra parses a leading-dash value as a flag before any command
// code sees it. That is generic CLI behavior, not specific to these keys, so
// the test documents the invocation rather than treating it as a defect.
func TestIntegration_GuardSettingsRejectNegativeCount(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-settings-negative")

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath,
		"settings", "set", "local.commit.guards.max_added_files", "--", "-5")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "stdout:\n%s", stdout)
	assert.Contains(t, stdout+stderr, "non-negative")
}
