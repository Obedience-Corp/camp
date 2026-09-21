//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gitIdentity = "-c user.email=test@test.com -c user.name=Test"

// setupSubmoduleWorktree builds a project that itself contains a submodule and
// adds a worktree with that submodule checked out. Git refuses to remove such
// a worktree without --force, however clean it is.
func setupSubmoduleWorktree(t *testing.T, tc *TestContainer, name string) (campPath, wtPath string) {
	t.Helper()
	campPath, projPath, _ := setupPruneCampaign(t, tc, name)
	libBare := "/test/" + name + "-lib.git"
	libSeed := "/test/" + name + "-lib-seed"

	tc.Shell(t, fmt.Sprintf(`
set -e
git init --bare %[1]s
git clone %[1]s %[2]s
printf '# Lib\n' > %[2]s/README.md
git -C %[2]s add . && git -C %[2]s %[4]s commit -m 'init lib'
git -C %[2]s branch -M main
git -C %[2]s push origin main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main
cd %[3]s
GIT_ALLOW_PROTOCOL=file git submodule add %[1]s lib
git %[4]s commit -m 'add lib submodule'
git push origin HEAD
`, libBare, libSeed, projPath, gitIdentity))

	out, err := tc.RunCampInDir(campPath, "project", "worktree", "add", "wt", "--project", "proj")
	require.NoError(t, err, "worktree add: %s", out)

	wtPath = campPath + "/projects/worktrees/proj/wt"
	tc.Shell(t, fmt.Sprintf(`cd %s && GIT_ALLOW_PROTOCOL=file git submodule update --init`, wtPath))
	return campPath, wtPath
}

func TestProjectWorktreeRemove_CleanSubmoduleWorktreeRemoved(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, wtPath := setupSubmoduleWorktree(t, tc, "wt-rm-sub-clean")

	out, err := tc.RunCampInDir(campPath, "project", "worktree", "remove", "wt", "--project", "proj")
	require.NoError(t, err, "remove clean submodule worktree: %s", out)
	assert.Contains(t, out, "submodules", "the forced removal must be reported: %s", out)

	exists, err := tc.CheckDirExists(wtPath)
	require.NoError(t, err)
	assert.False(t, exists, "worktree directory must be gone")
	assert.NotContains(t, tc.GitOutput(t, campPath+"/projects/proj", "worktree", "list"), "worktrees/proj/wt")
}

func TestProjectWorktreeRemove_DirtySubmoduleWorktreeRefusedUntilForced(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, wtPath := setupSubmoduleWorktree(t, tc, "wt-rm-sub-dirty")
	require.NoError(t, tc.WriteFile(wtPath+"/lib/scratch.txt", "unsaved\n"))

	out, err := tc.RunCampInDir(campPath, "project", "worktree", "remove", "wt", "--project", "proj")
	require.Error(t, err, "a dirty submodule must block removal: %s", out)
	assert.Contains(t, out, "uncommitted changes")

	exists, err := tc.CheckFileExists(wtPath + "/lib/scratch.txt")
	require.NoError(t, err)
	assert.True(t, exists, "the refused removal must leave the unsaved file in place")

	out, err = tc.RunCampInDir(campPath, "project", "worktree", "remove", "wt", "--project", "proj", "--force")
	require.NoError(t, err, "--force removes it: %s", out)
	exists, err = tc.CheckDirExists(wtPath)
	require.NoError(t, err)
	assert.False(t, exists, "--force must remove the worktree")
}

// The superproject is clean here: the submodule commit is recorded in the
// worktree's branch. But the submodule repository lives inside the worktree's
// git dir, so a commit that was never pushed exists nowhere else.
func TestProjectWorktreeRemove_UnpushedSubmoduleCommitRefused(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, wtPath := setupSubmoduleWorktree(t, tc, "wt-rm-sub-unpushed")

	tc.Shell(t, fmt.Sprintf(`
set -e
printf 'local only\n' > %[1]s/lib/local.txt
git -C %[1]s/lib add local.txt
git -C %[1]s/lib %[2]s commit -m 'local only'
git -C %[1]s add lib
git -C %[1]s %[2]s commit -m 'bump lib'
test -z "$(git -C %[1]s status --porcelain)"
`, wtPath, gitIdentity))

	out, err := tc.RunCampInDir(campPath, "project", "worktree", "remove", "wt", "--project", "proj")
	require.Error(t, err, "an unpushed submodule commit must block removal: %s", out)
	assert.Contains(t, out, "on no remote")
	assert.Contains(t, out, "lib")

	exists, err := tc.CheckDirExists(wtPath)
	require.NoError(t, err)
	assert.True(t, exists, "the refused removal must leave the worktree in place")
}
