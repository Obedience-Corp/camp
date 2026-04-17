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
	assert.Contains(t, output, "Committed changes to git")

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

	_, exitCode, err = tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git ls-files --error-unmatch projects/linked-app")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "linked project symlink should be tracked in the campaign repo")

	linkLog, exitCode, err := tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git log -1 --pretty=%s")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, strings.TrimSpace(linkLog), "Link: linked-app")

	campaignStatus, _, err := tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git status --porcelain")
	require.NoError(t, err)
	assert.Equal(t, "", strings.TrimSpace(campaignStatus), "campaign repo should stay clean after auto-committing the link")

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

	output, err := tc.RunCampInDir(linkedPath, "project", "link", "--campaign", "proj-link-target")
	require.NoError(t, err, "project link should succeed from the current directory when --campaign is provided")
	assert.Contains(t, output, "Linked project: outside-linked-app")
	assert.Contains(t, output, "Committed changes to git")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignPath+"/projects/outside-linked-app")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "linked project entry should be created in the selected campaign")

	linkLog, exitCode, err := tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git log -1 --pretty=%s")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, strings.TrimSpace(linkLog), "Link: outside-linked-app")
}

func TestProject_Link_PicksCampaignOutsideCurrentContext(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignA := "/campaigns/proj-link-pick-alpha"
	campaignB := "/campaigns/proj-link-pick-bravo"
	linkedPath := "/test/picker-linked-app"

	_, err := tc.InitCampaign(campaignA, "proj-link-pick-alpha", "product")
	require.NoError(t, err)
	_, err = tc.InitCampaign(campaignB, "proj-link-pick-bravo", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))
	campaignIDB := readCampaignID(t, tc, campaignB)

	output, err := tc.RunCampInteractiveInDir(linkedPath, "Switch to:", "bravo\r", "project", "link")
	require.NoError(t, err, "project link should open the campaign picker outside campaign context")
	assert.Contains(t, output, "Linked project: picker-linked-app")
	assert.Contains(t, output, "Committed changes to git")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignB+"/projects/picker-linked-app")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "selected campaign should receive the linked project symlink")

	_, exitCode, err = tc.ExecCommand("test", "-L", campaignA+"/projects/picker-linked-app")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "non-selected campaign should not receive the linked project symlink")

	marker, err := tc.ReadFile(linkedPath + "/.camp")
	require.NoError(t, err)
	assert.Contains(t, marker, "\"active_campaign_id\": \""+campaignIDB+"\"")
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

func TestProject_Commit_LinkedGitRepo(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-link-commit"
	linkedPath := "/test/linked-commit-app"

	_, err := tc.InitCampaign(campaignPath, "proj-link-commit", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	output, err := tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
	require.NoError(t, err)
	assert.Contains(t, output, "camp project commit")

	require.NoError(t, tc.WriteFile(linkedPath+"/README.md", "linked change\n"))

	commitOutput, err := tc.RunCampInDir(linkedPath, "project", "commit", "-m", "linked change")
	require.NoError(t, err, "linked git repo should support camp project commit")
	assert.Contains(t, commitOutput, "Project changes committed")

	logOutput, exitCode, err := tc.ExecCommand("sh", "-c", "cd "+linkedPath+" && git log -1 --pretty=%s")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, strings.TrimSpace(logOutput), "[OBEY-CAMPAIGN-")
	assert.Contains(t, strings.TrimSpace(logOutput), "linked change")
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

func TestProject_Link_RejectsLegacyMarkerFromAnotherCampaign(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignA := "/campaigns/proj-link-legacy-a"
	campaignB := "/campaigns/proj-link-legacy-b"
	linkedPath := "/test/legacy-linked-app"

	_, err := tc.InitCampaign(campaignA, "proj-link-legacy-a", "product")
	require.NoError(t, err)
	_, err = tc.InitCampaign(campaignB, "proj-link-legacy-b", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	legacyMarker := "{\n  \"version\": 1,\n  \"campaign_root\": \"" + campaignA + "\"\n}\n"
	require.NoError(t, tc.WriteFile(linkedPath+"/.camp", legacyMarker))

	output, err := tc.RunCampInDir(campaignB, "project", "link", linkedPath)
	require.Error(t, err, "link should reject a mismatched legacy marker")
	assert.Contains(t, output, "legacy .camp marker")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignB+"/projects/legacy-linked-app")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "rejected link should not create a symlink")
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
	assert.Contains(t, output, "Committed changes to git")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignPath+"/projects/remove-linked")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "linked project symlink should be removed")

	exists, err := tc.CheckFileExists(linkedPath + "/.camp")
	require.NoError(t, err)
	assert.False(t, exists, ".camp marker should be removed on unlink")

	_, exitCode, err = tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git ls-files --error-unmatch projects/remove-linked")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "linked project symlink should be removed from the campaign index")

	unlinkLog, exitCode, err := tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git log -1 --pretty=%s")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, strings.TrimSpace(unlinkLog), "Unlink: remove-linked")

	campaignStatus, _, err := tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git status --porcelain")
	require.NoError(t, err)
	assert.Equal(t, "", strings.TrimSpace(campaignStatus), "campaign repo should stay clean after auto-committing the unlink")
}

func TestProject_Unlink_RejectsNonLinkedProject(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-unlink-nonlinked"
	localRepo := "/test/unlink-nonlinked"

	_, err := tc.InitCampaign(campaignPath, "proj-unlink-nonlinked", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(localRepo))

	_, err = tc.RunCampInDir(campaignPath, "project", "add", "--local", localRepo)
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "project", "unlink", "unlink-nonlinked")
	require.Error(t, err, "unlink should reject normal submodule projects")
	assert.Contains(t, output, "not a linked project")
	assert.Contains(t, output, "camp project remove unlink-nonlinked")
}

func TestProject_Unlink_CurrentLinkedProjectCwd(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-unlink-cwd"
	linkedPath := "/test/unlink-cwd-linked"

	_, err := tc.InitCampaign(campaignPath, "proj-unlink-cwd", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	_, err = tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
	require.NoError(t, err)

	_, _, err = tc.ExecCommand("mkdir", "-p", linkedPath+"/src/pkg")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(linkedPath+"/src/pkg", "project", "unlink")
	require.NoError(t, err, "project unlink should infer the current linked project from cwd")
	assert.Contains(t, output, "Unlinked project: unlink-cwd-linked")
	assert.Contains(t, output, "Committed changes to git")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignPath+"/projects/unlink-cwd-linked")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "linked project symlink should be removed")
}

func TestProject_Remove_LinkedProjectDeleteBlocked(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/proj-remove-linked-delete"
	linkedPath := "/test/remove-linked-delete"

	_, err := tc.InitCampaign(campaignPath, "proj-remove-linked-delete", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	_, err = tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "project", "remove", "remove-linked-delete", "--delete", "--force")
	require.Error(t, err, "linked projects should not support remove --delete")
	assert.Contains(t, output, "linked projects can only be unlinked")

	_, exitCode, err := tc.ExecCommand("test", "-L", campaignPath+"/projects/remove-linked-delete")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "blocked remove --delete must not remove the symlink")
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

// TestProject_DetectFromLinkedMarkerUsesRegistry verifies that campaign
// detection from a linked project's .camp marker resolves the campaign root
// through the registry (not by walking the filesystem). The linked project is
// placed in a directory tree that contains no campaign ancestor — the only way
// detect can succeed is via the registry lookup.
//
// Equivalent to the deleted unit test
// internal/campaign/detect_test.go::TestDetect_FromLinkedProjectMarkerUsesRegistry,
// but exercised end-to-end via camp commands inside the container so the on-disk
// registry, marker file format, and detection path all run as they do in
// production.
func TestProject_DetectFromLinkedMarkerUsesRegistry(t *testing.T) {
	tc := GetSharedContainer(t)

	campaignPath := "/campaigns/detect-from-marker"
	linkedPath := "/test/detect-marker-linked"

	_, err := tc.InitCampaign(campaignPath, "detect-from-marker", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(linkedPath))

	_, err = tc.RunCampInDir(campaignPath, "project", "link", linkedPath)
	require.NoError(t, err, "project link should succeed")

	exists, err := tc.CheckFileExists(linkedPath + "/.camp")
	require.NoError(t, err)
	require.True(t, exists, "linked repo should have a .camp marker")

	// Run camp root from inside the linked project. The linked project has no
	// .campaign ancestor in its filesystem path — detection must resolve the
	// campaign root through the registry using the marker's active_campaign_id.
	output, err := tc.RunCampInDir(linkedPath, "root")
	require.NoError(t, err, "camp root should succeed from a linked project")

	got := strings.TrimSpace(output)
	assert.Equal(t, campaignPath, got, "camp root should resolve to the registered campaign root, not a parent walk")
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
