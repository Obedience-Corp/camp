//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupProjectRunCampaign creates a campaign with one or more local projects
// for project run tests.
func setupProjectRunCampaign(t *testing.T, tc *TestContainer, campaignPath, campaignName string, projects ...string) {
	t.Helper()

	_, err := tc.InitCampaign(campaignPath, campaignName, "product")
	require.NoError(t, err)

	for _, p := range projects {
		addLocalProject(t, tc, campaignPath, p)
	}
}

func TestProjectRun_ExplicitProject(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-explicit"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-explicit", "my-app")

	// camp project run -p my-app -- pwd
	output, err := tc.RunCampInDir(campaignPath, "project", "run", "-p", "my-app", "--", "pwd")
	require.NoError(t, err, "project run with explicit -p should succeed")
	assert.Contains(t, output, "projects/my-app", "should run inside project directory")
}

func TestProjectRun_ExplicitProjectLongFlag(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-long"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-long", "my-app")

	// camp project run --project my-app -- pwd
	output, err := tc.RunCampInDir(campaignPath, "project", "run", "--project", "my-app", "--", "pwd")
	require.NoError(t, err, "project run with --project should succeed")
	assert.Contains(t, output, "projects/my-app", "should run inside project directory")
}

func TestProjectRun_ExplicitProjectEqualsForm(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-equals"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-equals", "my-app")

	// camp project run --project=my-app -- pwd
	output, err := tc.RunCampInDir(campaignPath, "project", "run", "--project=my-app", "--", "pwd")
	require.NoError(t, err, "project run with --project=value should succeed")
	assert.Contains(t, output, "projects/my-app", "should run inside project directory")
}

func TestProjectRun_AutoDetectFromCwd(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-autodetect"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-autodetect", "my-app")

	// Run from inside the project directory — should auto-detect
	output, err := tc.RunCampInDir(campaignPath+"/projects/my-app", "project", "run", "--", "pwd")
	require.NoError(t, err, "project run from inside project should auto-detect")
	assert.Contains(t, output, "projects/my-app", "should run inside auto-detected project")
}

func TestProjectRun_AutoDetectFromSubdir(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-subdir"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-subdir", "my-app")

	// Create a subdirectory inside the project
	_, _, err := tc.ExecCommand("mkdir", "-p", campaignPath+"/projects/my-app/src/pkg")
	require.NoError(t, err)

	// Run from deep inside the project — should still auto-detect
	output, err := tc.RunCampInDir(campaignPath+"/projects/my-app/src/pkg", "project", "run", "--", "pwd")
	require.NoError(t, err, "project run from project subdirectory should auto-detect")
	assert.Contains(t, output, "projects/my-app", "should run inside project root")
}

func TestProjectRun_NoFlagsNoSeparator(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-noflag"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-noflag", "my-app")

	// Run without -- separator from inside project
	output, err := tc.RunCampInDir(campaignPath+"/projects/my-app", "project", "run", "pwd")
	require.NoError(t, err, "project run without -- should still work")
	assert.Contains(t, output, "projects/my-app", "should run inside project directory")
}

func TestProjectRun_ArbitraryCommand(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-arbitrary"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-arbitrary", "my-app")

	// Write a file into the project, then verify we can read it
	err := tc.WriteFile(campaignPath+"/projects/my-app/test.txt", "hello-from-project")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "project", "run", "-p", "my-app", "--", "cat", "test.txt")
	require.NoError(t, err, "should be able to run arbitrary commands")
	assert.Contains(t, output, "hello-from-project", "should read file from project directory")
}

func TestProjectRun_ShellFeatures(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-shell"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-shell", "my-app")

	// Test that shell features (pipes, etc.) work since we use sh -c
	output, err := tc.RunCampInDir(campaignPath+"/projects/my-app", "project", "run", "--", "echo", "hello-world")
	require.NoError(t, err, "echo should work")
	assert.Contains(t, output, "hello-world")
}

func TestProjectRun_MultipleProjects(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-multi"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-multi", "frontend", "backend")

	// Run in frontend
	output1, err := tc.RunCampInDir(campaignPath, "project", "run", "-p", "frontend", "--", "pwd")
	require.NoError(t, err)
	assert.Contains(t, output1, "projects/frontend", "should run in frontend")

	// Run in backend
	output2, err := tc.RunCampInDir(campaignPath, "project", "run", "-p", "backend", "--", "pwd")
	require.NoError(t, err)
	assert.Contains(t, output2, "projects/backend", "should run in backend")
}

func TestProjectRun_NoCommandError(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-nocmd"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-nocmd", "my-app")

	// No command after project flag — should error
	_, err := tc.RunCampInDir(campaignPath, "project", "run", "-p", "my-app")
	require.Error(t, err, "should fail when no command is specified")
	assert.Contains(t, err.Error(), "no command specified")
}

func TestProjectRun_InvalidProject(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-invalid"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-invalid", "my-app")

	// Non-existent project — should error
	_, err := tc.RunCampInDir(campaignPath, "project", "run", "-p", "nonexistent", "--", "pwd")
	require.Error(t, err, "should fail for non-existent project")
	assert.Contains(t, err.Error(), "not found", "error should mention project not found")
}

func TestProjectRun_NotInCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Run from outside any campaign
	_, err := tc.RunCampInDir("/test", "project", "run", "-p", "anything", "--", "pwd")
	require.Error(t, err, "should fail outside a campaign")
}

func TestProjectRun_CommandExitCode(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-exitcode"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-exitcode", "my-app")

	// Command that exits with non-zero code
	_, err := tc.RunCampInDir(campaignPath, "project", "run", "-p", "my-app", "--", "false")
	require.Error(t, err, "should propagate non-zero exit code from child command")
}

func TestProjectRun_FromCampaignRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-root"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-root", "my-app")

	// From campaign root (not in any project) with explicit -p
	output, err := tc.RunCampInDir(campaignPath, "project", "run", "-p", "my-app", "--", "pwd")
	require.NoError(t, err, "should work from campaign root with -p flag")
	assert.Contains(t, output, "projects/my-app")
}

func TestProjectRun_ShowsProjectLabel(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/pr-label"

	setupProjectRunCampaign(t, tc, campaignPath, "pr-label", "my-app")

	// The command prints "project: <path>" to stderr (merged into stdout by RunCampInDir)
	output, err := tc.RunCampInDir(campaignPath, "project", "run", "-p", "my-app", "--", "echo", "done")
	require.NoError(t, err)
	assert.Contains(t, output, "project:", "should show project label in output")
	assert.Contains(t, output, "projects/my-app", "label should contain project path")
	assert.Contains(t, output, "done", "command output should also appear")
}
