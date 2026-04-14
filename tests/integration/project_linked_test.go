//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProject_Link_GitRepo(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-link"
	linkedPath := "/test/linked-app"

	_, err := tc.InitCampaign(campaignPath, "proj-link", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))
	campaignID := readCampaignID(t, tc, campaignPath)

	output, err := tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
	require.NoError(t, err, "project link should succeed")
	assert.Contains(t, output, "Linked project: linked-app")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignPath+"/projects/linked-app")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "linked project entry should be a symlink")

	exists, err := tc.CheckFileExists(linkedPath + "/.camp")
	require.NoError(t, err)
	assert.True(t, exists, "linked repo should receive a .camp marker")

	marker, err := tc.ReadFile(linkedPath + "/.camp")
	require.NoError(t, err)
	assert.Contains(t, marker, "\"active_campaign_id\": \""+campaignID+"\"")
	assert.NotContains(t, marker, "\"campaign_root\"")
	assert.NotContains(t, marker, "\"project_name\"")

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

func TestProject_Link_TargetCampaignOutsideCurrentContext(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-link-target"
	linkedPath := "/test/outside-linked-app"

	_, err := tc.InitCampaign(campaignPath, "proj-link-target", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	output, err := tc.RunCampInDir("/test", "project", "link", linkedPath, "--campaign", "proj-link-target")
	require.NoError(t, err, "project link should succeed outside a campaign when --campaign is provided")
	assert.Contains(t, output, "Linked project: outside-linked-app")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignPath+"/projects/outside-linked-app")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "linked project entry should be created in the selected campaign")
}

func TestProject_Link_NonGitDir(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-link-dir"
	linkedPath := "/test/plain-linked-dir"

	_, err := tc.InitCampaign(campaignPath, "proj-link-dir", "product")
	require.NoError(t, err)

	_, _, err = tc.ExecCommand("mkdir", "-p", linkedPath)
	require.NoError(t, err)
	require.NoError(t, tc.WriteFile(linkedPath+"/package.json", "{}"))

	output, err := tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
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

func TestProject_Link_RejectsRepoAlreadyLinkedToAnotherCampaign(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignA := "/campaigns/proj-link-a"
	campaignB := "/campaigns/proj-link-b"
	linkedPath := "/test/shared-linked-app"

	_, err := tc.InitCampaign(campaignA, "proj-link-a", "product")
	require.NoError(t, err)
	_, err = tc.InitCampaign(campaignB, "proj-link-b", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))
	campaignIDA := readCampaignID(t, tc, campaignA)

	_, err = tc.RunCampInDir(campaignA, "project", "link", linkedPath)
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignB, "project", "link", linkedPath)
	require.Error(t, err, "second campaign should be rejected")
	assert.Contains(t, output, "already linked to another campaign")
	assert.Contains(t, output, campaignA)

	marker, err := tc.ReadFile(linkedPath + "/.camp")
	require.NoError(t, err)
	assert.Contains(t, marker, "\"active_campaign_id\": \""+campaignIDA+"\"")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignB+"/projects/shared-linked-app")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "second campaign should not create a symlink")
}

func TestProject_Link_RejectsDuplicateTargetWithinCampaign(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-link-dup-target"
	linkedPath := "/test/dup-target-linked-app"

	_, err := tc.InitCampaign(campaignPath, "proj-link-dup-target", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	_, err = tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "project", "link", linkedPath, "--name", "dup-target-alias")
	require.Error(t, err, "adding the same linked target under a second alias should fail")
	assert.Contains(t, output, "already linked as")
	assert.Contains(t, output, "dup-target-linked-app")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignPath+"/projects/dup-target-alias")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "duplicate alias should not create a second symlink")
}

func TestProject_Unlink_LinkedProject(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-unlink"
	linkedPath := "/test/remove-linked"

	_, err := tc.InitCampaign(campaignPath, "proj-unlink", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	_, err = tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "project", "unlink", "remove-linked")
	require.NoError(t, err, "project unlink should succeed")
	assert.Contains(t, output, "Unlinked project: remove-linked")

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
	_, err = tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
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
	_, err = tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
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
	_, err := tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "run", "run-linked-project", "build")
	require.NoError(t, err, "run should dispatch linked project recipes in the external repo")
	assert.Contains(t, output, "just-dispatch: build")
	assert.Contains(t, output, "just-workdir: "+linkedPath)
}

// TestProject_Link_RejectsDuplicateTargetInSameCampaign verifies that
// linking the same external directory twice into one campaign under different
// aliases is rejected up front. Without the guard, both symlinks would be
// created but the URL-based dedup in List() would silently drop one,
// hiding it from `project list`, `project run`, and `leverage`.
func TestProject_Link_RejectsDuplicateTargetInSameCampaign(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-link-dup"
	linkedPath := "/test/dup-target-app"

	_, err := tc.InitCampaign(campaignPath, "proj-link-dup", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	// First link under default alias (derived from path basename).
	firstOutput, err := tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
	require.NoError(t, err, "first project link should succeed")
	assert.Contains(t, firstOutput, "Linked project: dup-target-app")

	// Second link under a different --name must be rejected.
	secondOutput, err := tc.RunCampInDir(campaignPath, "project", "link", linkedPath, "--name", "other-alias")
	require.Error(t, err, "second project link with different --name should fail")
	assert.Contains(t, secondOutput, "already linked",
		"error message should explain that the target is already linked")
	assert.Contains(t, secondOutput, "dup-target-app",
		"error message should name the existing alias")

	// The second symlink must not have been created.
	_, exitCode, err := tc.ExecCommand("test", "-L", campaignPath+"/projects/other-alias")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "second alias symlink should not exist")

	// The first symlink must still exist and point at the target.
	_, exitCode, err = tc.ExecCommand("test", "-L", campaignPath+"/projects/dup-target-app")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "first alias symlink should still exist")

	// And `project list` must still show the surviving alias.
	listOutput, err := tc.RunCampInDir(campaignPath, "project", "list")
	require.NoError(t, err)
	assert.Contains(t, listOutput, "dup-target-app")
	assert.NotContains(t, listOutput, "other-alias",
		"rejected alias should not appear in project list")
}

// TestProject_Link_AllowsRelinkAfterUnlink verifies that the duplicate
// rejection is scoped to active duplicates only. After unlinking an alias,
// the same target can be re-linked under any name.
func TestProject_Link_AllowsRelinkAfterUnlink(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-link-relink"
	linkedPath := "/test/relink-target-app"

	_, err := tc.InitCampaign(campaignPath, "proj-link-relink", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	// Link under alias "alpha".
	_, err = tc.RunCampInDir(campaignPath, "project", "link", linkedPath, "--name", "alpha")
	require.NoError(t, err, "initial link under alpha should succeed")

	// Unlink alpha. The dedicated unlink command should remove only the
	// symlink and linked-project marker state.
	_, err = tc.RunCampInDir(campaignPath, "project", "unlink", "alpha")
	require.NoError(t, err, "unlinking alpha should succeed")

	// Re-link under a different alias "beta" — rejection must not fire
	// because there is no active alias pointing at the target any more.
	output, err := tc.RunCampInDir(campaignPath, "project", "link", linkedPath, "--name", "beta")
	require.NoError(t, err, "re-linking under beta after unlink should succeed")
	assert.Contains(t, output, "Linked project: beta")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignPath+"/projects/beta")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "beta symlink should be created")
}

func readCampaignID(t *testing.T, tc *TestContainer, campaignPath string) string {
	t.Helper()

	content, err := tc.ReadFile(campaignPath + "/.campaign/campaign.yaml")
	require.NoError(t, err)

	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "id: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "id: "))
		}
	}

	t.Fatalf("campaign ID not found in %s/.campaign/campaign.yaml", campaignPath)
	return ""
}
