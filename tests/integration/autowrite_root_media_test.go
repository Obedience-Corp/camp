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

// setupQuietGrowthCampaign tracks three files inside the mixed root: one
// matched by commit.guards.allow, one marked filter=lfs, and one with
// neither. Declaring the root is the first camp commit.
func setupQuietGrowthCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()
	campPath := setupMixedRootCampaign(t, tc, name)
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		cat >> .campaign/campaign.yaml <<'YAML'
    allow:
      - videos/my-video/allowed.md
YAML
		printf 'videos/my-video/*.bin filter=lfs diff=lfs merge=lfs -text\n' > .gitattributes
		printf 'allowed\n' > videos/my-video/allowed.md
		printf 'lfs\n' > videos/my-video/model.bin
		printf 'plain\n' > videos/my-video/plain.md
		git add .gitattributes videos/my-video/allowed.md videos/my-video/model.bin videos/my-video/plain.md
		git -c user.email=t@t -c user.name=t commit -q -m "track root files"
	`, campPath))
	first, err := tc.RunCampInDir(campPath, "commit", "-m", "declare the root")
	require.NoError(t, err, "output:\n%s", first)
	require.Contains(t, first, "is now an artifact root")
	settleQueue(t, tc, campPath)
	return campPath
}

func TestIntegration_TrackedGrowthInRootSkipsAllowedAndLFSPaths(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupQuietGrowthCampaign(t, tc, "growth-quiet-paths")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		for f in allowed.md model.bin plain.md; do
			dd if=/dev/zero of=videos/my-video/$f bs=1024 count=2048 2>/dev/null
		done
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "grow the root files")
	require.NoError(t, err, "output:\n%s", output)
	assert.Contains(t, output, "videos/my-video/plain.md is tracked and now",
		"a path neither allowed nor LFS-managed still gets the warning; output:\n%s", output)
	assert.NotContains(t, output, "allowed.md is tracked",
		"commit.guards.allow must silence the growth warning; output:\n%s", output)
	assert.NotContains(t, output, "model.bin is tracked",
		"an LFS-managed path must not be told to stop tracking; output:\n%s", output)
	committed := tc.GitOutput(t, campPath, "log", "-5", "--name-only", "--format=")
	for _, f := range []string{"allowed.md", "model.bin", "plain.md"} {
		assert.Contains(t, committed, "videos/my-video/"+f, "tracked files are always committed")
	}
}

func TestIntegration_AutoWriteKeepsQuietGrowthOnTheFastPath(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath := setupQuietGrowthCampaign(t, tc, "aw-growth-quiet-paths")
	configureWriter(t, tc, campPath, "plain")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		for f in allowed.md model.bin; do
			dd if=/dev/zero of=videos/my-video/$f bs=1024 count=2048 2>/dev/null
		done
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	combined := stdout + stderr
	require.Equal(t, 0, exitCode, "output:\n%s", combined)
	require.Contains(t, combined, "staged and queued", "output:\n%s", combined)
	assert.NotContains(t, combined, "Staging changes",
		"allowed and LFS-managed growth has nothing to report, so it must not force the foreground; output:\n%s", combined)
	assert.NotContains(t, combined, "is tracked and now", "output:\n%s", combined)
	settleQueue(t, tc, campPath)
	for _, f := range []string{"allowed.md", "model.bin"} {
		size := strings.TrimSpace(tc.GitOutput(t, campPath, "cat-file", "-s", "HEAD:videos/my-video/"+f))
		assert.Equal(t, "2097152", size, "the grown %s must be in HEAD", f)
	}

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		dd if=/dev/zero of=videos/my-video/plain.md bs=1024 count=2048 2>/dev/null
	`, campPath))
	stdout, stderr, exitCode, err = tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	combined = stdout + stderr
	require.Equal(t, 0, exitCode, "output:\n%s", combined)
	assert.Contains(t, combined, "videos/my-video/plain.md is tracked and now",
		"growth the guard reports must still take the foreground and print; output:\n%s", combined)
}

// A clean root is gitignored when it is declared. Excluding it again from
// the stage-everything add makes git refuse the whole add.
func TestIntegration_CommitAfterDeclaringACleanRootStagesTheRest(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "clean-root-then-commit")
	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		mkdir -p media
		printf 'seed' > media/seed.bin
	`, campPath))
	added, err := tc.RunCampInDir(campPath, "artifacts", "add", "media")
	require.NoError(t, err, "output:\n%s", added)
	require.Contains(t, added, "to .gitignore")

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "declare media and add notes")
	require.NoError(t, err, "output:\n%s", output)
	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", latestUserCommit(t, tc, campPath))
	assert.Contains(t, committed, "notes/one.md")
	assert.Contains(t, committed, ".campaign/artifacts.yaml")
	assert.NotContains(t, committed, "media/seed.bin")

	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		printf 'later\n' >> notes/two.md
		printf 'more' > media/more.bin
	`, campPath))
	output, err = tc.RunCampInDir(campPath, "commit", "-m", "a later edit")
	require.NoError(t, err, "a commit after the root is declared must still stage; output:\n%s", output)
	committed = tc.GitOutput(t, campPath, "show", "--name-only", "--format=", latestUserCommit(t, tc, campPath))
	assert.Contains(t, committed, "notes/two.md")
	assert.NotContains(t, committed, "media/")
}

// Size decides inside a mixed root. A small new file there, including one in
// a new subdirectory, is committed. Over-threshold media stays out and is
// tallied: the existing footage and the new b-roll.
const rootDropScript = `
		printf 'remember the b-roll\n' > videos/my-video/todo.md
		mkdir -p videos/my-video/ideas
		printf 'idea\n' > videos/my-video/ideas/idea.md
		dd if=/dev/zero of=videos/my-video/broll.mp4 bs=1024 count=2048 2>/dev/null
`

func assertRootDropCommitted(t *testing.T, tc *TestContainer, campPath, output string) {
	t.Helper()
	assertInHEAD(t, tc, campPath,
		[]string{"videos/my-video/todo.md", "videos/my-video/ideas/idea.md"},
		[]string{"videos/my-video/broll.mp4", "videos/my-video/footage.mp4"})
	assert.Contains(t, output, "videos/my-video/ +2 files (5.0 MB) kept out of git", "output:\n%s", output)
	assert.Contains(t, output, "Commit them anyway with --commit-large", "output:\n%s", output)
	assertOnlyRootMediaLeft(t, tc, campPath, "videos/my-video/footage.mp4", "videos/my-video/broll.mp4")
}

func TestIntegration_SmallFileInDeclaredRootIsCommitted(t *testing.T) {
	tc, campPath, output := rootCommit(t, "small-file-in-root", false, rootDropScript)
	assertRootDropCommitted(t, tc, campPath, output)
}

func TestIntegration_AutoWriteSmallFileInDeclaredRootIsCommitted(t *testing.T) {
	tc, campPath, output := rootCommit(t, "aw-small-file-in-root", true, rootDropScript)
	assertRootDropCommitted(t, tc, campPath, output)
}

// --commit-large forces over-threshold files into the commit, inside a
// declared root as anywhere else. --auto-write with it stages in the
// foreground, so the flag means the same thing on both paths.
func commitLargeAdmitsRootMedia(t *testing.T, name string, autoWrite bool) {
	t.Helper()
	tc := GetSharedContainer(t)
	if autoWrite {
		tc.EnableDeferral()
	}
	campPath := setupMixedRootCampaign(t, tc, name)
	if autoWrite {
		configureWriter(t, tc, campPath, "plain")
	}
	first, err := tc.RunCampInDir(campPath, "commit", "-m", "declare the root")
	require.NoError(t, err, "output:\n%s", first)
	settleQueue(t, tc, campPath)
	tc.Shell(t, fmt.Sprintf("set -e\ncd %s\n%s", campPath, rootDropScript))

	args := []string{"commit", "--commit-large", "-m", "commit the media"}
	if autoWrite {
		args = []string{"commit", "--commit-large", "--auto-write"}
	}
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, args...)
	require.NoError(t, err)
	output := stdout + stderr
	require.Equal(t, 0, exitCode, "output:\n%s", output)
	assert.NotContains(t, output, "kept out of git", "output:\n%s", output)
	settleQueue(t, tc, campPath)
	assertInHEAD(t, tc, campPath, []string{
		"videos/my-video/todo.md", "videos/my-video/ideas/idea.md",
		"videos/my-video/broll.mp4", "videos/my-video/footage.mp4",
	}, nil)
	assertOnlyRootMediaLeft(t, tc, campPath)
}

func TestIntegration_CommitLargeAdmitsRootMedia(t *testing.T) {
	commitLargeAdmitsRootMedia(t, "commit-large-root-media", false)
}

func TestIntegration_AutoWriteCommitLargeAdmitsRootMedia(t *testing.T) {
	commitLargeAdmitsRootMedia(t, "aw-commit-large-root-media", true)
}

// large_files decides root content the same way for camp status and camp
// commit. auto keeps over-threshold files out and status reports them as
// artifact content; off commits them and status says nothing; block refuses
// the commit, stages nothing, and status says nothing.
func largeFilesModeCommit(t *testing.T, name, mode string) (*TestContainer, string, string, string, int) {
	t.Helper()
	tc := GetSharedContainer(t)
	campPath := setupMixedRootCampaign(t, tc, name)
	first, err := tc.RunCampInDir(campPath, "commit", "-m", "declare the root")
	require.NoError(t, err, "output:\n%s", first)
	settleQueue(t, tc, campPath)
	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		printf '    large_files: "%s"\n' >> .campaign/campaign.yaml
		%s
	`, campPath, mode, rootDropScript))

	status, statusErr, statusExit, err := tc.RunCampSplitInDir(campPath, "status")
	require.NoError(t, err)
	require.Equal(t, 0, statusExit, "stderr:\n%s", statusErr)
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "-m", "mode "+mode)
	require.NoError(t, err)
	if exitCode == 0 {
		settleQueue(t, tc, campPath)
	}
	return tc, campPath, status, stdout + stderr, exitCode
}

func TestIntegration_LargeFilesAutoKeepsRootMediaOut(t *testing.T) {
	tc, campPath, status, output, exitCode := largeFilesModeCommit(t, "large-files-auto", "auto")
	require.Equal(t, 0, exitCode, "output:\n%s", output)
	assert.Contains(t, status, "kept out of git", "status:\n%s", status)
	assertRootDropCommitted(t, tc, campPath, output)
}

func TestIntegration_LargeFilesOffCommitsRootMedia(t *testing.T) {
	tc, campPath, status, output, exitCode := largeFilesModeCommit(t, "large-files-off", "off")
	require.Equal(t, 0, exitCode, "output:\n%s", output)
	assert.NotContains(t, status, "kept out of git", "status:\n%s", status)
	assert.NotContains(t, output, "kept out of git", "output:\n%s", output)
	assertInHEAD(t, tc, campPath, []string{
		"videos/my-video/todo.md", "videos/my-video/broll.mp4", "videos/my-video/footage.mp4",
	}, nil)
	assertOnlyRootMediaLeft(t, tc, campPath)
}

func TestIntegration_LargeFilesBlockRefusesRootMedia(t *testing.T) {
	tc, campPath, status, output, exitCode := largeFilesModeCommit(t, "large-files-block", "block")
	assert.NotEqual(t, 0, exitCode, "block must refuse a commit that keeps root media out; output:\n%s", output)
	assert.NotContains(t, status, "kept out of git", "status:\n%s", status)
	assert.Contains(t, output, "broll.mp4", "output:\n%s", output)
	assert.Empty(t, stagedPaths(t, tc, campPath), "a block refusal must stage nothing")
	assertInHEAD(t, tc, campPath, nil, []string{"videos/my-video/todo.md", "videos/my-video/broll.mp4"})
}

// A file named like a glob is captured as that one file, not as a pathspec
// that sweeps in every video in the camp.
const globNameScript = `
		printf 'not a video\n' > '*.mp4'
`

func TestIntegration_FileNamedLikeAGlobIsCommittedLiterally(t *testing.T) {
	tc, campPath, _ := rootCommit(t, "glob-name", false, globNameScript)
	assertInHEAD(t, tc, campPath, []string{"*.mp4"}, []string{"videos/my-video/footage.mp4"})
	assertOnlyRootMediaLeft(t, tc, campPath, "videos/my-video/footage.mp4")
}

func TestIntegration_AutoWriteFileNamedLikeAGlobIsCommittedLiterally(t *testing.T) {
	tc, campPath, _ := rootCommit(t, "aw-glob-name", true, globNameScript)
	assertInHEAD(t, tc, campPath, []string{"*.mp4"}, []string{"videos/my-video/footage.mp4"})
	assertOnlyRootMediaLeft(t, tc, campPath, "videos/my-video/footage.mp4")
}

// Small files the root plan commits count toward the bulk guard in the
// foreground too; the status walk does not list them.
func TestIntegration_BulkDropIntoDeclaredRootIsRefused(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupMixedRootCampaign(t, tc, "bulk-drop-in-root")
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		cat >> .campaign/campaign.yaml <<'YAML'
    max_added_files: 5
    bulk: block
YAML
	`, campPath))
	first, err := tc.RunCampInDir(campPath, "commit", "-m", "declare the root")
	require.NoError(t, err, "output:\n%s", first)
	require.Contains(t, first, "is now an artifact root")
	settleQueue(t, tc, campPath)

	tc.Shell(t, fmt.Sprintf("set -e\ncd %s\nfor i in 1 2 3 4 5 6; do printf 'x' > videos/my-video/frame$i.txt; done", campPath))
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "-m", "drop frames")
	require.NoError(t, err)
	combined := stdout + stderr
	assert.NotEqual(t, 0, exitCode, "a bulk drop into a root must be refused like any other; output:\n%s", combined)
	assert.Contains(t, combined, "videos/my-video", "output:\n%s", combined)
	assertInHEAD(t, tc, campPath, nil, []string{"videos/my-video/frame1.txt"})
	assert.Empty(t, stagedPaths(t, tc, campPath), "a bulk refusal must stage nothing")
}

// --commit-large silences tracked growth inside a root, as it silences the
// status walk's report outside one.
func TestIntegration_CommitLargeSilencesGrowthInARoot(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupMixedRootCampaign(t, tc, "commit-large-growth-in-root")
	first, err := tc.RunCampInDir(campPath, "commit", "-m", "declare the root")
	require.NoError(t, err, "output:\n%s", first)
	settleQueue(t, tc, campPath)
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		dd if=/dev/zero of=videos/my-video/script.md bs=1024 count=3072 2>/dev/null
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "--commit-large", "-m", "grow the script")
	require.NoError(t, err, "output:\n%s", output)
	assert.NotContains(t, output, "is tracked and now", "output:\n%s", output)
	size := strings.TrimSpace(tc.GitOutput(t, campPath, "cat-file", "-s", "HEAD:videos/my-video/script.md"))
	assert.Equal(t, "3145728", size)
}
