//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRemoveSubmoduleCampaign(t *testing.T, tc *TestContainer, name string) (campPath, projPath, barePath string) {
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
printf '# Proj\n' > %[2]s/README.md
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
GIT_ALLOW_PROTOCOL=file git submodule add %[2]s projects/subproj
git -C %[1]s commit -m 'add subproj'
`, campPath, barePath))

	projPath = campPath + "/projects/subproj"
	return campPath, projPath, barePath
}

func TestProjectRemove_UnpushedBranch_Refuses(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupRemoveSubmoduleCampaign(t, tc, "remove-unpushed")

	// Create a branch with a commit and do NOT push it.
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b unpushed-branch
printf 'secret work\n' > %[1]s/secret.txt
git -C %[1]s add secret.txt && git -C %[1]s commit -m 'unpushed commit'
git -C %[1]s checkout main
`, projPath))

	// Remove without --force should refuse.
	_, err := tc.RunCampInDir(campPath, "project", "remove", "subproj")
	assert.Error(t, err, "should refuse when unpushed branch exists")
	assert.Contains(t, err.Error(), "unpushed", "error should mention unpushed branches")
	assert.Contains(t, err.Error(), "unpushed-branch", "error should name the branch")

	// The .git/modules entry must still exist.
	exists, err := tc.CheckDirExists(campPath + "/.git/modules/projects/subproj")
	require.NoError(t, err)
	assert.True(t, exists, ".git/modules entry must not be deleted on refusal")
}

func TestProjectRemove_UnpushedBranch_ForceSucceeds(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupRemoveSubmoduleCampaign(t, tc, "remove-force")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b force-branch
printf 'will lose\n' > %[1]s/loseme.txt
git -C %[1]s add loseme.txt && git -C %[1]s commit -m 'will be lost'
git -C %[1]s checkout main
`, projPath))

	_, err := tc.RunCampInDir(campPath, "project", "remove", "subproj", "--force")
	require.NoError(t, err, "remove --force should succeed even with unpushed branch")

	exists, err := tc.CheckDirExists(campPath + "/.git/modules/projects/subproj")
	require.NoError(t, err)
	assert.False(t, exists, ".git/modules entry should be removed with --force")
}

func TestProjectRemove_AllPushed_Succeeds(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupRemoveSubmoduleCampaign(t, tc, "remove-pushed")

	// Create a branch, push it, then remove the project.
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b pushed-branch
printf 'pushed\n' > %[1]s/pushed.txt
git -C %[1]s add pushed.txt && git -C %[1]s commit -m 'pushed commit'
GIT_ALLOW_PROTOCOL=file git -C %[1]s push origin pushed-branch
git -C %[1]s checkout main
`, projPath))

	_, err := tc.RunCampInDir(campPath, "project", "remove", "subproj")
	require.NoError(t, err, "remove should succeed when all branches are pushed")

	exists, err := tc.CheckDirExists(campPath + "/.git/modules/projects/subproj")
	require.NoError(t, err)
	assert.False(t, exists, ".git/modules entry should be cleaned up after successful remove")
}
