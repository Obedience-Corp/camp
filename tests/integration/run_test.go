//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installMockJust creates a shell script at /usr/local/bin/just that echoes
// its arguments. This lets us verify dispatch without installing real just.
func installMockJust(tc *TestContainer) error {
	script := `#!/bin/sh
echo "just-dispatch: $@"
echo "just-workdir: $(pwd)"
`
	if err := tc.WriteFile("/usr/local/bin/just", script); err != nil {
		return err
	}
	_, _, err := tc.ExecCommand("chmod", "+x", "/usr/local/bin/just")
	return err
}

// setupRunTestCampaign creates a campaign with mock just and a project.
func setupRunTestCampaign(t *testing.T, tc *TestContainer, campaignPath, campaignName string) {
	t.Helper()

	_, err := tc.InitCampaign(campaignPath, campaignName, "product")
	require.NoError(t, err)

	err = installMockJust(tc)
	require.NoError(t, err)
}

// addLocalProject creates a git repo and adds it as a local project to the campaign.
func addLocalProject(t *testing.T, tc *TestContainer, campaignPath, projectName string) {
	t.Helper()

	repoPath := "/test/" + projectName
	err := tc.CreateGitRepo(repoPath)
	require.NoError(t, err)

	_, err = tc.RunCampInDir(campaignPath, "project", "add", repoPath, "--local", repoPath)
	require.NoError(t, err)
}

func TestRun_ProjectDispatch(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/run-dispatch"

	setupRunTestCampaign(t, tc, campaignPath, "run-dispatch")
	addLocalProject(t, tc, campaignPath, "my-app")

	// camp run my-app build → just build in projects/my-app/
	output, err := tc.RunCampInDir(campaignPath, "run", "my-app", "build")
	require.NoError(t, err, "run project dispatch should succeed")
	assert.Contains(t, output, "just-dispatch: build", "should dispatch 'just build'")
	assert.Contains(t, output, "projects/my-app", "should run from project directory")
}

func TestRun_ProjectDispatch_MultipleArgs(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/run-multi"

	setupRunTestCampaign(t, tc, campaignPath, "run-multi")
	addLocalProject(t, tc, campaignPath, "my-app")

	// camp run my-app test all → just test all in projects/my-app/
	output, err := tc.RunCampInDir(campaignPath, "run", "my-app", "test", "all")
	require.NoError(t, err, "run with multiple args should succeed")
	assert.Contains(t, output, "just-dispatch: test all", "should pass all args to just")
}

func TestRun_ProjectDispatch_NoRecipe(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/run-norecipe"

	setupRunTestCampaign(t, tc, campaignPath, "run-norecipe")
	addLocalProject(t, tc, campaignPath, "my-app")

	// camp run my-app → just (no args) in projects/my-app/
	output, err := tc.RunCampInDir(campaignPath, "run", "my-app")
	require.NoError(t, err, "run with project only should succeed")
	assert.Contains(t, output, "just-dispatch:", "should dispatch just with no args")
	assert.Contains(t, output, "projects/my-app", "should run from project directory")
}

func TestRun_FallbackToRawCommand(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/run-fallback"

	setupRunTestCampaign(t, tc, campaignPath, "run-fallback")

	// camp run ls → ls from campaign root (no project named "ls")
	output, err := tc.RunCampInDir(campaignPath, "run", "ls")
	require.NoError(t, err, "run with non-project arg should fall through to raw command")
	// ls from campaign root should show the projects/ directory
	assert.Contains(t, output, "projects", "raw ls should list campaign root contents")
}

func TestRun_NonProjectDir_FallsThrough(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/run-notproject"

	setupRunTestCampaign(t, tc, campaignPath, "run-notproject")

	// Create a plain directory in projects/ (no .git)
	_, _, err := tc.ExecCommand("mkdir", "-p", campaignPath+"/projects/plain-dir")
	require.NoError(t, err)

	// camp run plain-dir build → should NOT dispatch to just (no .git)
	// Instead falls through to raw command "plain-dir build" which will fail
	_, err = tc.RunCampInDir(campaignPath, "run", "plain-dir", "build")
	require.Error(t, err, "non-git directory should not trigger project dispatch")
}

func TestRun_MultipleProjects(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/run-multiple"

	setupRunTestCampaign(t, tc, campaignPath, "run-multiple")
	addLocalProject(t, tc, campaignPath, "frontend")
	addLocalProject(t, tc, campaignPath, "backend")

	// Dispatch to first project
	output1, err := tc.RunCampInDir(campaignPath, "run", "frontend", "build")
	require.NoError(t, err)
	assert.Contains(t, output1, "projects/frontend", "should run in frontend project")

	// Dispatch to second project
	output2, err := tc.RunCampInDir(campaignPath, "run", "backend", "test")
	require.NoError(t, err)
	assert.Contains(t, output2, "projects/backend", "should run in backend project")
}

func TestRun_FromSubdirectory(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/run-subdir"

	setupRunTestCampaign(t, tc, campaignPath, "run-subdir")
	addLocalProject(t, tc, campaignPath, "my-app")

	// Create a subdirectory to run from
	_, _, err := tc.ExecCommand("mkdir", "-p", campaignPath+"/docs/notes")
	require.NoError(t, err)

	// Run from deep inside the campaign — should still resolve project
	output, err := tc.RunCampInDir(campaignPath+"/docs/notes", "run", "my-app", "build")
	require.NoError(t, err, "run from subdirectory should still resolve project")
	assert.Contains(t, output, "just-dispatch: build")
	assert.Contains(t, output, "projects/my-app")
}

func TestRun_RawCommandFromRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/run-raw"

	setupRunTestCampaign(t, tc, campaignPath, "run-raw")

	// Raw command with pipes and shell features
	output, err := tc.RunCampInDir(campaignPath, "run", "echo", "hello")
	require.NoError(t, err)
	assert.Contains(t, output, "hello", "raw echo should work from campaign root")
}

func TestRun_NotInCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Run from a directory that is not a campaign
	_, err := tc.RunCampInDir("/test", "run", "ls")
	require.Error(t, err, "run outside campaign should fail")
}
