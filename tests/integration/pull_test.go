//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPull_RemovesStaleIndexLock(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/pull-lock"
	setupPullLockRootRepo(t, tc, campaignDir)

	tc.Shell(t, fmt.Sprintf(`
		git clone /test/root-remote.git /test/root-seed
		cd /test/root-seed
		printf 'remote content' > remote.txt
		git add remote.txt
		git commit -m 'remote change'
		git push origin main
		printf 'stale lock' > %s/.git/index.lock
	`, campaignDir))

	output, err := tc.RunCampInDir(campaignDir, "pull", "--ff-only")
	require.NoError(t, err, output)

	exists, err := tc.CheckFileExists(campaignDir + "/.git/index.lock")
	require.NoError(t, err)
	require.False(t, exists, "stale root index.lock should be removed")

	exists, err = tc.CheckFileExists(campaignDir + "/remote.txt")
	require.NoError(t, err)
	require.True(t, exists, "remote file should be pulled after stale lock cleanup")
}

func TestPullAll_RemovesSubmoduleStaleIndexLock(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/pull-all-lock"
	const subDir = campaignDir + "/projects/test-project"
	setupPullLockCampaignWithSubmodule(t, tc, campaignDir, subDir)

	tc.Shell(t, `
		git clone /test/submodule-remote.git /test/submodule-seed-2
		cd /test/submodule-seed-2
		printf 'remote submodule content' > pulled.txt
		git add pulled.txt
		git commit -m 'remote submodule change'
		git push origin main
		sub_git_dir="$(git -C /campaigns/pull-all-lock/projects/test-project rev-parse --absolute-git-dir)"
		printf 'stale lock' > "$sub_git_dir/index.lock"
	`)

	output, err := tc.RunCampInDir(campaignDir, "pull", "all", "--ff-only")
	require.NoError(t, err, output)

	subGitDir := strings.TrimSpace(tc.GitOutput(t, subDir, "rev-parse", "--absolute-git-dir"))
	exists, err := tc.CheckFileExists(subGitDir + "/index.lock")
	require.NoError(t, err)
	require.False(t, exists, "stale submodule index.lock should be removed")

	exists, err = tc.CheckFileExists(subDir + "/pulled.txt")
	require.NoError(t, err)
	require.True(t, exists, "submodule file should be pulled after stale lock cleanup")
}

func TestPullAll_DivergentBranchesRebase(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/pull-diverge-rebase"
	setupPullLockRootRepo(t, tc, campaignDir)

	tc.Shell(t, `
		git clone /test/root-remote.git /test/root-seed-diverge
		cd /test/root-seed-diverge
		printf 'remote line' > remote.txt
		git add remote.txt
		git commit -m 'remote diverge'
		git push origin main
	`)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'local line' > local.txt
		git add local.txt
		git commit -m 'local diverge'
	`, campaignDir))

	output, err := tc.RunCampInDir(campaignDir, "pull", "all", "--rebase")
	require.NoError(t, err, "pull all --rebase should succeed on divergent branches; output:\n%s", output)

	for _, path := range []string{"remote.txt", "local.txt"} {
		exists, err := tc.CheckFileExists(campaignDir + "/" + path)
		require.NoError(t, err)
		require.True(t, exists, "%s should be present after rebase pull", path)
	}
}

func TestPullAll_DivergentBranchesDefaultFails(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/pull-diverge-default"
	setupPullLockRootRepo(t, tc, campaignDir)

	tc.Shell(t, `
		git clone /test/root-remote.git /test/root-seed-divdef
		cd /test/root-seed-divdef
		printf 'remote line' > remote_def.txt
		git add remote_def.txt
		git commit -m 'remote diverge default'
		git push origin main
	`)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'local line' > local_def.txt
		git add local_def.txt
		git commit -m 'local diverge default'
	`, campaignDir))

	output, err := tc.RunCampInDir(campaignDir, "pull", "all")
	require.Error(t, err, "default pull all should fail on divergent branches")
	require.Contains(t, output, "branches diverged")
}

func TestPullAll_FfOnlyOverride(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/pull-ffonly"
	setupPullLockRootRepo(t, tc, campaignDir)

	tc.Shell(t, `
		git clone /test/root-remote.git /test/root-seed-ff
		cd /test/root-seed-ff
		printf 'remote ff content' > ff.txt
		git add ff.txt
		git commit -m 'remote ff'
		git push origin main
	`)

	output, err := tc.RunCampInDir(campaignDir, "pull", "all", "--ff-only")
	require.NoError(t, err, "pull all --ff-only should succeed when no divergence; output:\n%s", output)

	exists, err := tc.CheckFileExists(campaignDir + "/ff.txt")
	require.NoError(t, err)
	require.True(t, exists, "ff.txt should be present after ff-only pull")
}

func TestPullAll_FfOnlyDivergentBranchesFails(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/pull-ffonly-diverge"
	setupPullLockRootRepo(t, tc, campaignDir)

	tc.Shell(t, `
		git clone /test/root-remote.git /test/root-seed-ff-diverge
		cd /test/root-seed-ff-diverge
		printf 'remote line' > remote_ff.txt
		git add remote_ff.txt
		git commit -m 'remote ff diverge'
		git push origin main
	`)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'local line' > local_ff.txt
		git add local_ff.txt
		git commit -m 'local ff diverge'
	`, campaignDir))

	output, err := tc.RunCampInDir(campaignDir, "pull", "all", "--ff-only")
	require.Error(t, err, "pull all --ff-only should fail on divergent branches")
	require.Contains(t, output, "Not possible to fast-forward")
}

func TestPullAll_RebaseConflictAutoAborts(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/pull-rebase-conflict"
	setupPullLockRootRepo(t, tc, campaignDir)

	tc.Shell(t, `
		git clone /test/root-remote.git /test/root-seed-conflict
		cd /test/root-seed-conflict
		printf 'remote version' > conflict.txt
		git add conflict.txt
		git commit -m 'remote adds conflict.txt'
		git push origin main
	`)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'local version' > conflict.txt
		git add conflict.txt
		git commit -m 'local adds conflict.txt'
	`, campaignDir))

	output, err := tc.RunCampInDir(campaignDir, "pull", "all", "--rebase")
	require.Error(t, err, "rebase pull should fail on conflict")
	require.Contains(t, output, "conflict (aborted rebase)")

	status := tc.GitOutput(t, campaignDir, "status")
	require.NotContains(t, status, "rebase in progress", "repo should not be in mid-rebase state after auto-abort")

	branch := tc.GitOutput(t, campaignDir, "rev-parse", "--abbrev-ref", "HEAD")
	require.NotEqual(t, "HEAD", branch, "repo should not be left detached after auto-abort")
}

func setupPullLockRootRepo(t *testing.T, tc *TestContainer, campaignDir string) {
	t.Helper()

	_, err := tc.InitCampaign(campaignDir, "Pull Lock", "")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		git init --bare --initial-branch=main /test/root-remote.git
		cd %s
		git branch -M main
		git remote add origin /test/root-remote.git
		git push -u origin main
	`, campaignDir))
}

func setupPullLockCampaignWithSubmodule(t *testing.T, tc *TestContainer, campaignDir, subDir string) {
	t.Helper()

	tc.Shell(t, `
		git init --bare --initial-branch=main /test/submodule-remote.git
		git clone /test/submodule-remote.git /test/submodule-seed
		cd /test/submodule-seed
		printf '# Test Project' > README.md
		git add README.md
		git commit -m 'initial submodule commit'
		git push origin main
	`)

	_, err := tc.InitCampaign(campaignDir, "Pull All Lock", "")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		git branch -M main
		GIT_ALLOW_PROTOCOL=file git submodule add /test/submodule-remote.git projects/test-project
		git commit -m 'add submodule'
		git -C %s branch --set-upstream-to=origin/main main
	`, campaignDir, subDir))
}
