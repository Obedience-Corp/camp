//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProject_AddLink_GitRepo(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-link"
	linkedPath := "/test/linked-app"

	_, err := tc.InitCampaign(campaignPath, "proj-link", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	output, err := tc.RunCampInDir(campaignPath, "project", "add", "--link", linkedPath)
	require.NoError(t, err, "project add --link should succeed")
	assert.Contains(t, output, "Linked project: linked-app")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignPath+"/projects/linked-app")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "linked project entry should be a symlink")

	exists, err := tc.CheckFileExists(linkedPath + "/.camp")
	require.NoError(t, err)
	assert.True(t, exists, "linked repo should receive a .camp marker")

	marker, err := tc.ReadFile(linkedPath + "/.camp")
	require.NoError(t, err)
	assert.Contains(t, marker, "\"campaign_root\": \""+campaignPath+"\"")
	assert.Contains(t, marker, "\"project_name\": \"linked-app\"")

	gitmodulesExists, err := tc.CheckFileExists(campaignPath + "/.gitmodules")
	require.NoError(t, err)
	assert.False(t, gitmodulesExists, "linked projects should not modify .gitmodules")

	campaignStatus, _, err := tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git status --porcelain")
	require.NoError(t, err)
	assert.Equal(t, "", strings.TrimSpace(campaignStatus), "campaign repo should stay clean after linking")

	linkedStatus, _, err := tc.ExecCommand("sh", "-c", "cd "+linkedPath+" && git status --porcelain")
	require.NoError(t, err)
	assert.Equal(t, "", strings.TrimSpace(linkedStatus), "linked repo should keep .camp untracked via info/exclude")

	listOutput, err := tc.RunCampInDir(campaignPath, "project", "list")
	require.NoError(t, err)
	assert.Contains(t, listOutput, "linked-app")
	assert.Contains(t, listOutput, "linked")
}

func TestProject_AddLink_NonGitDir(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-link-dir"
	linkedPath := "/test/plain-linked-dir"

	_, err := tc.InitCampaign(campaignPath, "proj-link-dir", "product")
	require.NoError(t, err)

	_, _, err = tc.ExecCommand("mkdir", "-p", linkedPath)
	require.NoError(t, err)
	require.NoError(t, tc.WriteFile(linkedPath+"/package.json", "{}"))

	output, err := tc.RunCampInDir(campaignPath, "project", "add", "--link", linkedPath)
	require.NoError(t, err)
	assert.Contains(t, output, "Linked project: plain-linked-dir")
	assert.Contains(t, output, "Git:")

	listOutput, err := tc.RunCampInDir(campaignPath, "project", "list")
	require.NoError(t, err)
	assert.Contains(t, listOutput, "plain-linked-dir")
	assert.Contains(t, listOutput, "linked-non-git")

	_, err = tc.RunCampInDir(campaignPath, "project", "commit", "--project", "plain-linked-dir", "-m", "should fail")
	require.Error(t, err, "linked non-git project commit should fail")
	assert.Contains(t, err.Error(), "linked non-git")
}

func TestProject_Remove_LinkedProject(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-unlink"
	linkedPath := "/test/remove-linked"

	_, err := tc.InitCampaign(campaignPath, "proj-unlink", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	_, err = tc.RunCampInDir(campaignPath, "project", "add", "--link", linkedPath)
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "project", "remove", "remove-linked")
	require.NoError(t, err, "linked project remove should unlink successfully")
	assert.Contains(t, output, "Linked project unlinked")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignPath+"/projects/remove-linked")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "linked project symlink should be removed")

	exists, err := tc.CheckFileExists(linkedPath + "/.camp")
	require.NoError(t, err)
	assert.False(t, exists, ".camp marker should be removed on unlink")

	campaignStatus, _, err := tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git status --porcelain")
	require.NoError(t, err)
	assert.Equal(t, "", strings.TrimSpace(campaignStatus), "campaign repo should stay clean after unlinking")
}

func TestProjectRun_AutoDetectFromLinkedCwd(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-linked-autodetect"
	linkedPath := "/test/linked-run"

	_, err := tc.InitCampaign(campaignPath, "pr-linked-autodetect", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))
	_, err = tc.RunCampInDir(campaignPath, "project", "add", "--link", linkedPath)
	require.NoError(t, err)

	_, _, err = tc.ExecCommand("mkdir", "-p", linkedPath+"/src/pkg")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(linkedPath+"/src/pkg", "project", "run", "--", "pwd")
	require.NoError(t, err, "project run should auto-detect linked project from cwd")
	assert.Contains(t, output, "project:")
	assert.Contains(t, output, "projects/linked-run")
	assert.Contains(t, output, linkedPath)
}

func TestGo_FromLinkedProjectCwd(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/go-linked"
	linkedPath := "/test/go-linked-project"

	_, err := tc.InitCampaign(campaignPath, "go-linked", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))
	_, err = tc.RunCampInDir(campaignPath, "project", "add", "--link", linkedPath)
	require.NoError(t, err)

	output, err := tc.RunCampInDir(linkedPath, "go", "p", "go-linked-project", "--print")
	require.NoError(t, err, "camp go should detect campaign context from linked cwd")
	assert.Contains(t, output, campaignPath+"/projects/go-linked-project")
}

func TestRun_ProjectDispatch_LinkedProject(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/run-linked"
	linkedPath := "/test/run-linked-project"

	setupRunTestCampaign(t, tc, campaignPath, "run-linked")
	require.NoError(t, tc.CreateGitRepo(linkedPath))
	_, err := tc.RunCampInDir(campaignPath, "project", "add", "--link", linkedPath)
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "run", "run-linked-project", "build")
	require.NoError(t, err, "run should dispatch linked project recipes in the external repo")
	assert.Contains(t, output, "just-dispatch: build")
	assert.Contains(t, output, "just-workdir: "+linkedPath)
}
