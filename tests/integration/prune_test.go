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

func setupPruneCampaign(t *testing.T, tc *TestContainer, name string) (campPath, projPath, barePath string) {
	t.Helper()
	campPath = "/campaigns/" + name
	barePath = "/test/" + name + "-origin.git"
	seedDir := "/test/" + name + "-seed"

	tc.Shell(t, fmt.Sprintf(`
set -e
git init --bare %[1]s
git clone %[1]s %[2]s
git -C %[2]s config user.email test@test.com
git -C %[2]s config user.name Test
printf '# Project\n' > %[2]s/README.md
git -C %[2]s add . && git -C %[2]s commit -m 'init'
git -C %[2]s branch -M main
git -C %[2]s push origin main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main
`, barePath, seedDir))

	_, err := tc.InitCampaign(campPath, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
set -e
cd %[1]s
GIT_ALLOW_PROTOCOL=file git submodule add %[2]s projects/proj
git -C %[1]s commit -m 'add proj'
`, campPath, barePath))

	projPath = campPath + "/projects/proj"
	return campPath, projPath, barePath
}

func TestPrune_MergedBranchPruned(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-merged")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b merged-feature
printf 'new\n' > %[1]s/feature.txt
git -C %[1]s add feature.txt && git -C %[1]s commit -m 'add feature'
git -C %[1]s checkout main
git -C %[1]s merge --no-ff merged-feature -m 'Merge merged-feature'
`, projPath))

	out, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "merged-feature")

	branchOut := tc.Shell(t, fmt.Sprintf(`git -C %s branch`, projPath))
	assert.NotContains(t, branchOut, "merged-feature")
}

func TestPrune_UnmergedBranchSkipped(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-unmerged")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b unmerged-feature
printf 'wip\n' > %[1]s/wip.txt
git -C %[1]s add wip.txt && git -C %[1]s commit -m 'wip'
git -C %[1]s checkout main
`, projPath))

	_, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)

	branchOut := tc.Shell(t, fmt.Sprintf(`git -C %s branch`, projPath))
	assert.Contains(t, branchOut, "unmerged-feature", "unmerged branch must still exist")
}

func TestPrune_DirtyWorktreeBranchSkipped(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-dirty-wt")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b dirty-wt-branch
printf 'content\n' > %[1]s/content.txt
git -C %[1]s add content.txt && git -C %[1]s commit -m 'content'
git -C %[1]s checkout main
git -C %[1]s merge --no-ff dirty-wt-branch -m 'Merge dirty-wt-branch'
git -C %[1]s worktree add %[2]s/projects/worktrees/proj/dirty-wt-branch dirty-wt-branch
printf 'uncommitted\n' > %[2]s/projects/worktrees/proj/dirty-wt-branch/uncommitted.txt
`, projPath, campPath))

	out, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "dirty", "output should mention dirty worktree")

	exists, err := tc.CheckDirExists(campPath + "/projects/worktrees/proj/dirty-wt-branch")
	require.NoError(t, err)
	assert.True(t, exists, "dirty worktree must not be removed")
}

func TestPrune_RemoteDelete_MergedBranchDeletedOnOrigin(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, barePath := setupPruneCampaign(t, tc, "prune-remote-del")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b remote-feature
printf 'remote\n' > %[1]s/remote.txt
git -C %[1]s add remote.txt && git -C %[1]s commit -m 'remote feature'
GIT_ALLOW_PROTOCOL=file git -C %[1]s push origin remote-feature
git -C %[1]s checkout main
git -C %[1]s merge --no-ff remote-feature -m 'Merge remote-feature'
GIT_ALLOW_PROTOCOL=file git -C %[1]s fetch --prune origin
`, projPath))

	// deleteRemoteBranches prompts for [y/N]; pipe "y" via shell to simulate confirmation.
	tc.Shell(t, fmt.Sprintf(
		`cd %s && printf 'y\n' | /camp project prune --project proj --force --remote-delete`,
		campPath))

	remoteOut := tc.Shell(t, fmt.Sprintf(`git --git-dir %s branch`, barePath))
	assert.NotContains(t, remoteOut, "remote-feature", "merged branch must be deleted from origin")
}

func TestPrune_DryRun_NoDeletions(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-dryrun")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b dry-branch
printf 'dry\n' > %[1]s/dry.txt
git -C %[1]s add dry.txt && git -C %[1]s commit -m 'dry'
git -C %[1]s checkout main
git -C %[1]s merge --no-ff dry-branch -m 'Merge dry-branch'
`, projPath))

	out, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--force", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "would")

	branchOut := tc.Shell(t, fmt.Sprintf(`git -C %s branch`, projPath))
	assert.Contains(t, branchOut, "dry-branch", "dry-run must not delete")
}
