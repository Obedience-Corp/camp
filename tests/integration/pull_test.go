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
