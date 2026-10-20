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

// A repeat commit must not walk an already-declared artifact root, and
// --auto-write must return with the commit queued. The new large file stays
// out of the commit. A small note beside it still lands.
func TestIntegration_AutoWriteSkipsDeclaredArtifactRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath := setupMixedRootCampaign(t, tc, "aw-skip-root")
	configureWriter(t, tc, campPath, "plain")

	first, err := tc.RunCampInDir(campPath, "commit", "-m", "declare the root")
	require.NoError(t, err, "output:\n%s", first)
	require.Contains(t, first, "is now an artifact root")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		dd if=/dev/zero of=videos/my-video/broll.mp4 bs=1024 count=3072 2>/dev/null
		printf 'caption\n' > caption.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	combined := stdout + stderr
	assert.Contains(t, combined, "staged and queued")
	assert.Contains(t, combined, "kept out of git")

	drainJobs(t, tc, campPath)
	committed := tc.GitOutput(t, campPath, "log", "-5", "--name-only", "--format=")
	assert.Contains(t, committed, "caption.md")
	assert.NotContains(t, committed, "broll.mp4")
	assert.NotContains(t, committed, "footage.mp4")
	status := tc.GitOutput(t, campPath, "status", "--porcelain")
	assert.NotContains(t, status, "caption.md",
		"after the job lands, the committed file must not be a staged deletion or untracked; status:\n%s", status)
	assert.NotRegexp(t, "(?m)^D ", status)
}

// A new over-threshold file outside any declared root still prints before
// the command returns. That report is the reason this case stays foreground.
func TestIntegration_AutoWritePrintsANewArtifactRootBeforeReturn(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath := setupGuardCampaign(t, tc, "aw-new-root")
	configureWriter(t, tc, campPath, "plain")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media/renders
		dd if=/dev/zero of=media/renders/frame.exr bs=1024 count=3072 2>/dev/null
		printf 'note\n' > note.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	combined := stdout + stderr
	assert.Contains(t, combined, "media/renders/ is now an artifact root",
		"the first exclusion has to print before the command returns; output:\n%s", combined)
	if strings.Contains(combined, "staged and queued") {
		drainJobs(t, tc, campPath)
	}
	// A manifest follow-up may land on top of the content commit. Look at the
	// recent history rather than only HEAD.
	committed := tc.GitOutput(t, campPath, "log", "-5", "--name-only", "--format=")
	assert.Contains(t, committed, "note.md")
	assert.NotContains(t, committed, "frame.exr")
}

func TestIntegration_AutoWriteCommitsASymlinkAndACampDirectory(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "aw-symlink-skill")
	configureWriter(t, tc, campPath, "plain")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'target\n' > target.md
		ln -s target.md link.md
		mkdir -p .campaign/skills/demo
		printf 'name: demo\n' > .campaign/skills/demo/SKILL.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	if strings.Contains(stdout+stderr, "staged and queued") {
		drainJobs(t, tc, campPath)
	}
	committed := tc.GitOutput(t, campPath, "log", "-5", "--name-only", "--format=")
	assert.Contains(t, committed, "link.md")
	assert.Contains(t, committed, ".campaign/skills/demo/SKILL.md")
	status := tc.GitOutput(t, campPath, "status", "--porcelain")
	assert.NotContains(t, status, "link.md", "status:\n%s", status)
	assert.NotContains(t, status, "SKILL.md", "status:\n%s", status)
}

func TestIntegration_MoveIntoDeclaredRootIsCommitted(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupMixedRootCampaign(t, tc, "move-into-root")
	first, err := tc.RunCampInDir(campPath, "commit", "-m", "declare the root")
	require.NoError(t, err, "output:\n%s", first)
	drainJobs(t, tc, campPath)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p notes
		printf 'carry me\n' > notes/readme.md
		git add notes/readme.md
		git -c user.email=t@t -c user.name=t commit -q -m "track the note"
		mv notes/readme.md videos/my-video/readme.md
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "move the note into the root")
	require.NoError(t, err, "output:\n%s", output)
	history := tc.GitOutput(t, campPath, "log", "-3", "--name-status", "--format=")
	assert.Contains(t, history, "videos/my-video/readme.md", "history:\n%s", history)
	status := tc.GitOutput(t, campPath, "status", "--porcelain")
	assert.NotContains(t, status, "videos/my-video/readme.md", "the moved file must not be left untracked; status:\n%s", status)
}

func TestIntegration_TrackedGrowthInsideDeclaredRootIsReported(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupMixedRootCampaign(t, tc, "growth-in-root")
	first, err := tc.RunCampInDir(campPath, "commit", "-m", "declare the root")
	require.NoError(t, err, "output:\n%s", first)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		dd if=/dev/zero of=videos/my-video/script.md bs=1024 count=3072 2>/dev/null
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "grow the script")
	require.NoError(t, err, "output:\n%s", output)
	assert.Contains(t, output, "videos/my-video/script.md is tracked and now")
	assert.Contains(t, output, "Tracked files are always committed")
	committed := tc.GitOutput(t, campPath, "log", "-3", "--name-only", "--format=")
	assert.Contains(t, committed, "videos/my-video/script.md")
}
