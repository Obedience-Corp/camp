//go:build integration
// +build integration

package integration

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A repeat commit into an already-declared artifact root takes the fast
// path: --auto-write returns with the commit queued. The new large file
// stays out of the commit. A small note beside it still lands.
func TestIntegration_AutoWriteSkipsDeclaredArtifactRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath := setupMixedRootCampaign(t, tc, "aw-skip-root")
	configureWriter(t, tc, campPath, "plain")

	first, err := tc.RunCampInDir(campPath, "commit", "-m", "declare the root")
	require.NoError(t, err, "output:\n%s", first)
	require.Contains(t, first, "is now an artifact root")
	settleQueue(t, tc, campPath)

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

// rootCommit declares the mixed root, runs prepare, and commits on the
// requested path. The queue is settled before prepare and after the commit,
// so HEAD and the status are final when it returns.
//
// autoWrite takes the --auto-write fast path, which must queue without the
// foreground stage. Otherwise the commit stages everything in the
// foreground.
func rootCommit(t *testing.T, name string, autoWrite bool, prepare string) (*TestContainer, string, string) {
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
	require.Contains(t, first, "is now an artifact root")
	settleQueue(t, tc, campPath)

	before := tc.Shell(t, fmt.Sprintf(
		"set -e\ncd %s\n%s\necho '--- untracked'\ngit ls-files -o --exclude-standard --directory --no-empty-directory\necho '--- status'\ngit status --porcelain",
		campPath, prepare))
	t.Logf("before the commit:\n%s", before)

	args := []string{"commit", "-m", "commit in the root"}
	if autoWrite {
		args = []string{"commit", "--auto-write"}
	}
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, args...)
	require.NoError(t, err)
	combined := stdout + stderr
	require.Equal(t, 0, exitCode, "output:\n%s", combined)
	if autoWrite {
		require.Contains(t, combined, "staged and queued", "output:\n%s", combined)
		require.NotContains(t, combined, "Staging changes",
			"the commit must be captured on the fast path, not the foreground stage; output:\n%s", combined)
	}
	settleQueue(t, tc, campPath)
	return tc, campPath, combined
}

// assertOnlyRootMediaLeft checks that the status holds nothing but the
// declared root's untracked media, which stays out of git by design.
func assertOnlyRootMediaLeft(t *testing.T, tc *TestContainer, campPath string, media ...string) {
	t.Helper()
	allowed := map[string]bool{}
	for _, m := range media {
		allowed["?? "+m] = true
	}
	status := tc.GitOutput(t, campPath, "status", "--porcelain")
	var left []string
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !allowed[line] {
			left = append(left, line)
		}
	}
	assert.Empty(t, left, "only the root's media may stay out of git; status:\n%s", status)
}

func assertInHEAD(t *testing.T, tc *TestContainer, campPath string, present, absent []string) {
	t.Helper()
	head := tc.GitOutput(t, campPath, "ls-tree", "-r", "--name-only", "HEAD")
	for _, p := range present {
		assert.Regexp(t, "(?m)^"+regexp.QuoteMeta(p)+"$", head, "%s must be in HEAD", p)
	}
	for _, p := range absent {
		assert.NotRegexp(t, "(?m)^"+regexp.QuoteMeta(p)+"$", head, "%s must not be in HEAD", p)
	}
}

// settleQueue serves every queued job under this container's own machine
// identity and waits until the lane is idle and the manifest record is
// committed.
//
// A drain waits only for blocking jobs. A worker also queues the manifest
// record after it marks a commit job done, so an empty queue alone can be
// read in that gap; the lane lock is held until the worker exits.
func settleQueue(t *testing.T, tc *TestContainer, campPath string) {
	t.Helper()
	machine := strings.TrimSpace(tc.Shell(t, "hostname"))
	deadline := time.Now().Add(90 * time.Second)
	for {
		settleJobs(t, tc, machine, campPath)
		busy := strings.TrimSpace(tc.Shell(t, fmt.Sprintf(`
			cd %s
			find .campaign/cache/jobs -maxdepth 1 -name 'worker-*.lock' 2>/dev/null
			find .campaign/cache/jobs/pending .campaign/cache/jobs/running -name '*.json' 2>/dev/null
			git status --porcelain -- .campaign/artifacts/manifests 2>/dev/null
		`, campPath)))
		if busy == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job queue never went idle:\n%s", busy)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// When the fast path cannot queue, the foreground stage prints the tally;
// the fast path must not have printed it already.
func TestIntegration_AutoWriteFallbackPrintsTheTallyOnce(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath := setupMixedRootCampaign(t, tc, "aw-fallback-tally-once")
	configureWriter(t, tc, campPath, "plain")
	first, err := tc.RunCampInDir(campPath, "commit", "-m", "declare the root")
	require.NoError(t, err, "output:\n%s", first)
	settleQueue(t, tc, campPath)
	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		mkdir -p .campaign/cache/jobs/pending/%%2E/0000001.json
		printf 'caption\n' > caption.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write", "--no-drain")
	require.NoError(t, err)
	output := stdout + stderr
	require.Equal(t, 0, exitCode, "output:\n%s", output)
	assert.Contains(t, output, "staging in the foreground", "output:\n%s", output)
	assert.Equal(t, 1, strings.Count(output, "kept out of git"), "the tally must print once; output:\n%s", output)
	assertInHEAD(t, tc, campPath, []string{"caption.md"}, []string{"videos/my-video/footage.mp4"})
}

// setupAutoWriteCampaign is a campaign with no artifact roots, deferral on,
// and a writer, settled so the fast path is the only thing left to decide.
func setupAutoWriteCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, name)
	configureWriter(t, tc, campPath, "plain")
	out, err := tc.RunCampInDir(campPath, "commit", "-m", "configure the writer")
	require.NoError(t, err, "output:\n%s", out)
	settleQueue(t, tc, campPath)
	return campPath
}

func requireFastPath(t *testing.T, tc *TestContainer, campPath string) string {
	t.Helper()
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	combined := stdout + stderr
	require.Equal(t, 0, exitCode, "output:\n%s", combined)
	require.Contains(t, combined, "staged and queued", "output:\n%s", combined)
	require.NotContains(t, combined, "Staging changes", "output:\n%s", combined)
	settleQueue(t, tc, campPath)
	return combined
}

// A new symlink is committed by the fast path itself. A regular-file-only
// size check would drop it.
func TestIntegration_AutoWriteCommitsASymlink(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupAutoWriteCampaign(t, tc, "aw-symlink")
	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		printf 'target\n' > target.md
		ln -s target.md link.md
	`, campPath))
	requireFastPath(t, tc, campPath)
	entry := tc.GitOutput(t, campPath, "ls-tree", "HEAD", "link.md")
	assert.True(t, strings.HasPrefix(entry, "120000 "), "link.md must be committed as a symlink; ls-tree: %q", entry)
	assert.Empty(t, tc.GitOutput(t, campPath, "status", "--porcelain"))
}

// A rename staged with git mv lists both paths, so the captured tree drops
// the old one instead of keeping it beside the new.
func TestIntegration_AutoWriteCommitsAStagedRename(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupAutoWriteCampaign(t, tc, "aw-git-mv")
	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		mkdir -p notes
		printf 'a\n' > notes/a.md
		git add notes/a.md
		git -c user.email=t@t -c user.name=t commit -q -m "track a"
		git mv notes/a.md notes/b.md
	`, campPath))
	requireFastPath(t, tc, campPath)
	assertInHEAD(t, tc, campPath, []string{"notes/b.md"}, []string{"notes/a.md"})
	assert.Empty(t, tc.GitOutput(t, campPath, "status", "--porcelain"))
}

// Untracked files dropped into an already-tracked directory are listed one
// by one, so the fast path counts them against the bulk guard itself.
func TestIntegration_AutoWriteBulkDropGoesToTheForeground(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath := setupGuardCampaign(t, tc, "aw-bulk-drop")
	writeGuardConfig(t, tc, campPath, "    max_added_files: 5\n    bulk: block")
	configureWriter(t, tc, campPath, "plain")
	out, err := tc.RunCampInDir(campPath, "commit", "-m", "track the notes")
	require.NoError(t, err, "output:\n%s", out)
	settleQueue(t, tc, campPath)

	tc.Shell(t, fmt.Sprintf("set -e\ncd %s\nfor i in 1 2 3 4; do printf 'x' > notes/below$i.md; done", campPath))
	requireFastPath(t, tc, campPath)
	assertInHEAD(t, tc, campPath, []string{"notes/below4.md"}, nil)

	tc.Shell(t, fmt.Sprintf("set -e\ncd %s\nfor i in 1 2 3 4 5; do printf 'x' > notes/at$i.md; done", campPath))
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	combined := stdout + stderr
	assert.Contains(t, combined, "Staging changes", "a bulk drop must reach the foreground guard; output:\n%s", combined)
	assert.NotEqual(t, 0, exitCode, "bulk: block must refuse the drop; output:\n%s", combined)
	assert.NotContains(t, combined, "staged and queued", "output:\n%s", combined)
	assertInHEAD(t, tc, campPath, nil, []string{"notes/at1.md"})
	assert.Empty(t, stagedPaths(t, tc, campPath), "a bulk refusal must stage nothing")
}

// A merge in progress has a second parent and maybe conflict stages, which
// a tree queued on one parent would drop. The commit goes to the foreground.
func TestIntegration_AutoWriteFallsBackDuringAMerge(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupAutoWriteCampaign(t, tc, "aw-during-merge")
	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		git checkout -q -b side
		printf 'side\n' > side.md
		git add side.md
		git -c user.email=t@t -c user.name=t commit -q -m side
		git checkout -q -
		printf 'main\n' > main.md
		git add main.md
		git -c user.email=t@t -c user.name=t commit -q -m main
		git -c user.email=t@t -c user.name=t merge -q --no-commit --no-ff side
		test -f .git/MERGE_HEAD
		printf 'during\n' > during.md
	`, campPath))
	stdout, stderr, _, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	combined := stdout + stderr
	assert.Contains(t, combined, "Staging changes",
		"a merge in progress must not be queued as a one-parent tree; output:\n%s", combined)
}

// A queue that cannot be written puts the index back and stages in the
// foreground, which then writes the message itself.
//
// A directory squatting on the next job file name makes every enqueue fail
// while the rest of the queue still reads. --no-drain keeps the pre-commit
// drain from waiting on it.
func TestIntegration_AutoWriteFallsBackWhenTheQueueCannotBeWritten(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupAutoWriteCampaign(t, tc, "aw-queue-broken")
	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		mkdir -p .campaign/cache/jobs/pending/%%2E/0000001.json
		printf 'still lands\n' > landed.md
	`, campPath))
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write", "--no-drain")
	require.NoError(t, err)
	combined := stdout + stderr
	require.Equal(t, 0, exitCode, "output:\n%s", combined)
	assert.Contains(t, combined, "staging in the foreground", "output:\n%s", combined)
	assert.Contains(t, combined, "Staging changes", "output:\n%s", combined)
	assertInHEAD(t, tc, campPath, []string{"landed.md"}, nil)
	assert.Empty(t, tc.GitOutput(t, campPath, "status", "--porcelain"))
	backups := tc.Shell(t, fmt.Sprintf("ls %s/.git/ | grep -c camp-before-queue || true", campPath))
	assert.Equal(t, "0", strings.TrimSpace(backups), "the index backup must not be left behind")
}
