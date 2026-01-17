//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProject_AddLocal(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/proj-add", "proj-add", "product")
	require.NoError(t, err)

	// Create a git repo to add
	err = tc.CreateGitRepo("/test/local-project")
	require.NoError(t, err)

	// Add the local project (source arg required even with --local)
	output, err := tc.RunCampInDir("/campaigns/proj-add", "project", "add", "/test/local-project", "--local", "/test/local-project")
	require.NoError(t, err, "project add --local should succeed")
	assert.Contains(t, output, "local-project", "output should mention project name")

	// Verify project was added to projects directory
	exists, err := tc.CheckDirExists("/campaigns/proj-add/projects/local-project")
	require.NoError(t, err)
	assert.True(t, exists, "project directory should exist")
}

func TestProject_AddLocal_CustomName(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup
	_, err := tc.InitCampaign("/campaigns/proj-name", "proj-name", "product")
	require.NoError(t, err)

	err = tc.CreateGitRepo("/test/orig-name")
	require.NoError(t, err)

	// Add with custom name (source arg required even with --local)
	output, err := tc.RunCampInDir("/campaigns/proj-name", "project", "add", "/test/orig-name", "--local", "/test/orig-name", "--name", "custom-name")
	require.NoError(t, err)
	assert.Contains(t, output, "custom-name", "output should use custom name")

	// Verify project exists with custom name
	exists, err := tc.CheckDirExists("/campaigns/proj-name/projects/custom-name")
	require.NoError(t, err)
	assert.True(t, exists, "project should exist with custom name")
}

func TestProject_List(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign with projects
	_, err := tc.InitCampaign("/campaigns/proj-list", "proj-list", "product")
	require.NoError(t, err)

	// Add multiple projects
	projects := []string{"proj-a", "proj-b", "proj-c"}
	for _, name := range projects {
		err = tc.CreateGitRepo("/test/" + name)
		require.NoError(t, err)
		_, err = tc.RunCampInDir("/campaigns/proj-list", "project", "add", "/test/"+name, "--local", "/test/"+name)
		require.NoError(t, err)
	}

	// List projects
	output, err := tc.RunCampInDir("/campaigns/proj-list", "project", "list")
	require.NoError(t, err, "project list should succeed")

	// Verify all projects appear
	for _, name := range projects {
		assert.Contains(t, output, name, "list should contain project %s", name)
	}
}

func TestProject_List_Empty(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign without projects
	_, err := tc.InitCampaign("/campaigns/proj-empty", "proj-empty", "product")
	require.NoError(t, err)

	// List projects (should be empty or show message)
	output, err := tc.RunCampInDir("/campaigns/proj-empty", "project", "list")
	require.NoError(t, err, "project list should succeed even with no projects")
	// Output might be empty or contain "no projects" message
	_ = output
}

func TestProject_Remove(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign with a project
	_, err := tc.InitCampaign("/campaigns/proj-remove", "proj-remove", "product")
	require.NoError(t, err)

	err = tc.CreateGitRepo("/test/to-remove")
	require.NoError(t, err)

	_, err = tc.RunCampInDir("/campaigns/proj-remove", "project", "add", "/test/to-remove", "--local", "/test/to-remove")
	require.NoError(t, err)

	// Verify project exists
	exists, err := tc.CheckDirExists("/campaigns/proj-remove/projects/to-remove")
	require.NoError(t, err)
	require.True(t, exists, "project should exist before removal")

	// Remove the project
	output, err := tc.RunCampInDir("/campaigns/proj-remove", "project", "remove", "to-remove")
	require.NoError(t, err, "project remove should succeed")
	assert.Contains(t, output, "to-remove", "output should mention removed project")

	// Verify project was removed
	exists, err = tc.CheckDirExists("/campaigns/proj-remove/projects/to-remove")
	require.NoError(t, err)
	assert.False(t, exists, "project directory should not exist after removal")
}

func TestProject_Remove_NotFound(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign without the project
	_, err := tc.InitCampaign("/campaigns/proj-notfound", "proj-notfound", "product")
	require.NoError(t, err)

	// Try to remove non-existent project
	output, err := tc.RunCampInDir("/campaigns/proj-notfound", "project", "remove", "nonexistent")
	require.Error(t, err, "removing non-existent project should fail")
	assert.Contains(t, strings.ToLower(output), "not found", "error should mention not found")
}

func TestProject_AlreadyExists(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign with a project
	_, err := tc.InitCampaign("/campaigns/proj-dup", "proj-dup", "product")
	require.NoError(t, err)

	err = tc.CreateGitRepo("/test/duplicate")
	require.NoError(t, err)

	_, err = tc.RunCampInDir("/campaigns/proj-dup", "project", "add", "/test/duplicate", "--local", "/test/duplicate")
	require.NoError(t, err)

	// Try to add again
	output, err := tc.RunCampInDir("/campaigns/proj-dup", "project", "add", "/test/duplicate", "--local", "/test/duplicate")
	require.Error(t, err, "adding duplicate project should fail")
	assert.Contains(t, strings.ToLower(output), "exists", "error should mention already exists")
}

func TestProject_Help(t *testing.T) {
	tc := GetSharedContainer(t)

	output, err := tc.RunCamp("project", "--help")
	require.NoError(t, err, "project --help should succeed")
	assert.Contains(t, output, "add", "help should list add subcommand")
	assert.Contains(t, output, "list", "help should list list subcommand")
	assert.Contains(t, output, "remove", "help should list remove subcommand")
}

func TestProject_NotInCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Create a git repo outside campaign
	err := tc.CreateGitRepo("/test/orphan")
	require.NoError(t, err)

	// Try to add project when not in a campaign
	output, err := tc.RunCampInDir("/test", "project", "add", "/test/orphan", "--local", "/test/orphan")
	require.Error(t, err, "project add should fail outside campaign")
	assert.Contains(t, strings.ToLower(output), "not inside a campaign", "error should mention not in campaign")
}

func TestProject_NotAGitRepo(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/proj-notgit", "proj-notgit", "product")
	require.NoError(t, err)

	// Create a regular directory (not a git repo)
	_, _, err = tc.ExecCommand("mkdir", "-p", "/test/notgitrepo")
	require.NoError(t, err)

	// Try to add non-git directory
	output, err := tc.RunCampInDir("/campaigns/proj-notgit", "project", "add", "/test/notgitrepo", "--local", "/test/notgitrepo")
	require.Error(t, err, "adding non-git directory should fail")
	assert.Contains(t, strings.ToLower(output), "not a git repository", "error should mention not a git repo")
}
