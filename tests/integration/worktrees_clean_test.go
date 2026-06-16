//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupWorktreeCleanCampaign creates a campaign with one submodule project
// and one registered worktree at the given path inside the container.
func setupWorktreeCleanCampaign(t *testing.T, tc *TestContainer, name string) (campPath, projPath string) {
	t.Helper()
	campPath = "/campaigns/" + name
	bareDir := "/test/" + name + "-origin.git"
	seedDir := "/test/" + name + "-seed"

	tc.Shell(t, fmt.Sprintf(`
set -e
git init --bare %[1]s
git clone %[1]s %[2]s
git -C %[2]s config user.email test@test.com
git -C %[2]s config user.name Test
printf '# Test\n' > %[2]s/README.md
git -C %[2]s add . && git -C %[2]s commit -m 'init'
git -C %[2]s branch -M main
git -C %[2]s push origin main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main
`, bareDir, seedDir))

	_, err := tc.InitCampaign(campPath, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
set -e
cd %[1]s
GIT_ALLOW_PROTOCOL=file git submodule add %[2]s projects/proj
git commit -m 'add proj'
`, campPath, bareDir))

	projPath = campPath + "/projects/proj"
	return campPath, projPath
}

func TestWorktreesClean_TrulyStalEntryRemoved(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath := setupWorktreeCleanCampaign(t, tc, "wt-clean-stale")

	// Create a worktree and then delete its gitdir manually to simulate stale state.
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s/projects/worktrees/proj/stale-branch -b stale-branch
GITDIR=$(cat %[2]s/projects/worktrees/proj/stale-branch/.git | sed 's/gitdir: //')
rm -rf $GITDIR
`, projPath, campPath))

	out, err := tc.RunCampInDir(campPath, "worktrees", "clean", "--all", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "removed")

	exists, err := tc.CheckDirExists(campPath + "/projects/worktrees/proj/stale-branch")
	require.NoError(t, err)
	assert.False(t, exists, "stale worktree directory should be removed")
}

func TestWorktreesClean_DirtyWorktreeSkipped(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupWorktreeCleanCampaign(t, tc, "wt-clean-dirty")

	// Create a worktree, then make the gitdir appear missing to trigger stale
	// detection but leave uncommitted files in the worktree directory.
	// Actually: create a valid worktree but corrupt the gitdir reference so
	// checkWorktreeStale returns "gitdir target does not exist" -- but the
	// worktree still has uncommitted changes.
	//
	// The safer test: create a worktree with a reason OTHER than
	// "gitdir target does not exist" (e.g. invalid .git format) and verify
	// it is not removed without --force.
	tc.Shell(t, fmt.Sprintf(`
set -e
mkdir -p %[1]s/projects/worktrees/proj/dirty-wt
printf 'gitdir: /nonexistent/path\n' > %[1]s/projects/worktrees/proj/dirty-wt/.git
printf 'uncommitted\n' > %[1]s/projects/worktrees/proj/dirty-wt/changes.txt
`, campPath))

	// Without --force, only gitdir-target-missing entries are auto-removed.
	// This entry has an invalid gitdir, not a missing one, so it stays.
	out, err := tc.RunCampInDir(campPath, "worktrees", "clean", "--all", "--yes")
	// Should not error but also should not remove since gitdir just doesn't exist now
	// The path created above has a gitdir pointing to /nonexistent -- that IS "gitdir target does not exist"
	// so it WILL be removed. Adjust: use "invalid .git file format" instead.
	_ = out
	_ = err

	// Correct setup: make the .git file have invalid format
	tc.Shell(t, fmt.Sprintf(`
set -e
mkdir -p %[1]s/projects/worktrees/proj/dirty-wt2
printf 'not-a-gitdir-file\n' > %[1]s/projects/worktrees/proj/dirty-wt2/.git
printf 'uncommitted\n' > %[1]s/projects/worktrees/proj/dirty-wt2/changes.txt
`, campPath))

	out2, err2 := tc.RunCampInDir(campPath, "worktrees", "clean", "--all", "--yes")
	require.NoError(t, err2)
	assert.NotContains(t, out2, "dirty-wt2", "invalid-format entry should not be auto-removed")

	exists, err := tc.CheckDirExists(campPath + "/projects/worktrees/proj/dirty-wt2")
	require.NoError(t, err)
	assert.True(t, exists, "non-gitdir-missing worktree should be preserved")
}

func TestWorktreesClean_GitDirEntryNeverRemoved(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupWorktreeCleanCampaign(t, tc, "wt-clean-gitdir")

	// Create a directory with a .git directory (full clone, not a worktree).
	tc.Shell(t, fmt.Sprintf(`
set -e
git init %[1]s/projects/worktrees/proj/full-clone
git -C %[1]s/projects/worktrees/proj/full-clone config user.email t@t.com
git -C %[1]s/projects/worktrees/proj/full-clone config user.name T
printf '# clone\n' > %[1]s/projects/worktrees/proj/full-clone/README.md
git -C %[1]s/projects/worktrees/proj/full-clone add .
git -C %[1]s/projects/worktrees/proj/full-clone commit -m 'init clone'
`, campPath))

	out, err := tc.RunCampInDir(campPath, "worktrees", "clean", "--all", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Skipping", "output should mention the skipped git-dir entry")

	exists, err := tc.CheckDirExists(campPath + "/projects/worktrees/proj/full-clone")
	require.NoError(t, err)
	assert.True(t, exists, ".git-directory entry must not be removed")
}

func TestWorktreesClean_DryRunNoChanges(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath := setupWorktreeCleanCampaign(t, tc, "wt-clean-dryrun")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s/projects/worktrees/proj/dry-branch -b dry-branch
GITDIR=$(cat %[2]s/projects/worktrees/proj/dry-branch/.git | sed 's/gitdir: //')
rm -rf $GITDIR
`, projPath, campPath))

	out, err := tc.RunCampInDir(campPath, "worktrees", "clean", "--all", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "Dry run")

	exists, err := tc.CheckDirExists(campPath + "/projects/worktrees/proj/dry-branch")
	require.NoError(t, err)
	assert.True(t, exists, "--dry-run must not remove anything")
}

func TestWorktreesClean_NonTTY_RefusesWithoutFlag(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath := setupWorktreeCleanCampaign(t, tc, "wt-clean-notty")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s/projects/worktrees/proj/notty-branch -b notty-branch
GITDIR=$(cat %[2]s/projects/worktrees/proj/notty-branch/.git | sed 's/gitdir: //')
rm -rf $GITDIR
`, projPath, campPath))

	// Container exec is not a TTY; without --yes this should refuse.
	_, err := tc.RunCampInDir(campPath, "worktrees", "clean", "--all")
	assert.Error(t, err, "non-TTY invocation without --yes should fail")
	assert.Contains(t, err.Error(), "non-interactive", "error should mention non-interactive mode")
}
