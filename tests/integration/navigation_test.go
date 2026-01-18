//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGo_NotInCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Try to use go command outside a campaign
	output, err := tc.RunCampInDir("/test", "go", "--help")
	// --help should work even outside campaign
	require.NoError(t, err, "camp go --help should work outside campaign")
	assert.Contains(t, output, "go", "help should show go command info")
}

func TestGo_NotInCampaign_NoArgs(t *testing.T) {
	tc := GetSharedContainer(t)

	// Try to use go command without args outside campaign
	output, err := tc.RunCampInDir("/test", "go")
	require.Error(t, err, "camp go should fail outside campaign without args")
	assert.Contains(t, strings.ToLower(output), "not inside a campaign", "error should mention not in campaign")
}

func TestGo_DirectJumpToProject(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign and add a project
	_, err := tc.InitCampaign("/campaigns/nav-test", "nav-test", "product")
	require.NoError(t, err)

	// Create a git repo to add as project
	err = tc.CreateGitRepo("/test/myproject")
	require.NoError(t, err)

	// Add project (source arg required even with --local)
	_, err = tc.RunCampInDir("/campaigns/nav-test", "project", "add", "/test/myproject", "--local", "/test/myproject")
	require.NoError(t, err)

	// Test fuzzy find in projects - use "p myproject" syntax (space-separated, not colon)
	output, err := tc.RunCampInDir("/campaigns/nav-test", "go", "p", "myproject", "--print")
	require.NoError(t, err, "camp go p myproject --print should succeed")
	assert.Contains(t, output, "myproject", "output should contain project path")
}

func TestGo_CategoryShortcuts(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/cat-test", "cat-test", "product")
	require.NoError(t, err)

	// Test category shortcuts - only test categories that exist by default (projects is created by init)
	t.Run("p", func(t *testing.T) {
		// Using --print flag to get path without TUI
		output, err := tc.RunCampInDir("/campaigns/cat-test", "go", "p", "--print")
		// This may error if no projects exist, but should at least return a path or "no targets"
		if err != nil {
			assert.Contains(t, strings.ToLower(output), "no targets", "error should mention no targets")
		} else {
			assert.Contains(t, output, "projects", "output should contain projects path")
		}
	})

	t.Run("projects", func(t *testing.T) {
		output, err := tc.RunCampInDir("/campaigns/cat-test", "go", "projects", "--print")
		if err != nil {
			assert.Contains(t, strings.ToLower(output), "no targets", "error should mention no targets")
		} else {
			assert.Contains(t, output, "projects", "output should contain projects path")
		}
	})

	// Test category that doesn't exist (festivals) - should get "directory not found" error
	t.Run("f_missing", func(t *testing.T) {
		output, err := tc.RunCampInDir("/campaigns/cat-test", "go", "f", "--print")
		require.Error(t, err, "should error when category directory doesn't exist")
		assert.Contains(t, strings.ToLower(output), "not found", "error should mention not found")
	})
}

func TestGo_ShellInit(t *testing.T) {
	tc := GetSharedContainer(t)

	// Test shell-init for different shells
	shells := []string{"bash", "zsh", "fish"}

	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			output, err := tc.RunCamp("shell-init", shell)
			require.NoError(t, err, "camp shell-init %s should succeed", shell)
			assert.NotEmpty(t, output, "shell init output should not be empty")
			// Output should contain function definition
			assert.Contains(t, output, "camp", "shell init should define camp function")
		})
	}
}

func TestGo_ShellInit_InvalidShell(t *testing.T) {
	tc := GetSharedContainer(t)

	output, err := tc.RunCamp("shell-init", "powershell")
	require.Error(t, err, "camp shell-init should fail for unsupported shell")
	assert.Contains(t, strings.ToLower(output), "unsupported", "error should mention unsupported")
}

func TestGo_PrintFlag(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign with a project
	_, err := tc.InitCampaign("/campaigns/print-test", "print-test", "product")
	require.NoError(t, err)

	err = tc.CreateGitRepo("/test/printproj")
	require.NoError(t, err)

	_, err = tc.RunCampInDir("/campaigns/print-test", "project", "add", "/test/printproj", "--local", "/test/printproj")
	require.NoError(t, err)

	// Test --print flag outputs path without cd (use space-separated syntax)
	output, err := tc.RunCampInDir("/campaigns/print-test", "go", "p", "printproj", "--print")
	require.NoError(t, err)
	assert.Contains(t, output, "printproj", "print flag should output project path")
}

func TestGo_Version(t *testing.T) {
	tc := GetSharedContainer(t)

	output, err := tc.RunCamp("version")
	require.NoError(t, err, "camp version should succeed")
	assert.Contains(t, output, "camp", "version output should contain camp")
}

func TestGo_Help(t *testing.T) {
	tc := GetSharedContainer(t)

	output, err := tc.RunCamp("--help")
	require.NoError(t, err, "camp --help should succeed")
	assert.Contains(t, output, "init", "help should list init command")
	assert.Contains(t, output, "go", "help should list go command")
	assert.Contains(t, output, "project", "help should list project command")
}

func TestGo_LastLocationNoHistory(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/last-loc-test", "last-loc-test", "product")
	require.NoError(t, err)

	// First time with no history should go to campaign root
	output, err := tc.RunCampInDir("/campaigns/last-loc-test", "go", "--print")
	require.NoError(t, err, "camp go should succeed on first run")
	assert.Contains(t, output, "last-loc-test", "should return campaign root path when no history")
}

func TestGo_LastLocationAfterNavigation(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign and project
	_, err := tc.InitCampaign("/campaigns/last-loc-nav", "last-loc-nav", "product")
	require.NoError(t, err)

	err = tc.CreateGitRepo("/test/navproject")
	require.NoError(t, err)

	_, err = tc.RunCampInDir("/campaigns/last-loc-nav", "project", "add", "/test/navproject", "--local", "/test/navproject")
	require.NoError(t, err)

	// First navigation to projects
	output1, err := tc.RunCampInDir("/campaigns/last-loc-nav", "go", "p", "--print")
	require.NoError(t, err, "go p should succeed")
	assert.Contains(t, output1, "projects", "first go p should return projects path")

	// Second call to go without args should return last location (projects)
	output2, err := tc.RunCampInDir("/campaigns/last-loc-nav", "go", "--print")
	require.NoError(t, err, "go without args should succeed")
	assert.Contains(t, output2, "projects", "go without args should return last location (projects)")
}

func TestGo_RootFlagIgnoresHistory(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign and navigate to projects
	_, err := tc.InitCampaign("/campaigns/root-flag-test", "root-flag-test", "product")
	require.NoError(t, err)

	err = tc.CreateGitRepo("/test/rootflagproj")
	require.NoError(t, err)

	_, err = tc.RunCampInDir("/campaigns/root-flag-test", "project", "add", "/test/rootflagproj", "--local", "/test/rootflagproj")
	require.NoError(t, err)

	// Navigate to projects
	_, err = tc.RunCampInDir("/campaigns/root-flag-test", "go", "p", "--print")
	require.NoError(t, err)

	// --root flag should ignore history and go to campaign root
	output, err := tc.RunCampInDir("/campaigns/root-flag-test", "go", "--root", "--print")
	require.NoError(t, err, "go --root should succeed")
	assert.Contains(t, output, "root-flag-test", "go --root should return campaign root path")
	assert.NotContains(t, output, "projects", "go --root should not return projects path")
}

func TestGo_MultipleNavigations(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/multi-nav-test", "multi-nav-test", "product")
	require.NoError(t, err)

	err = tc.CreateGitRepo("/test/multiproj")
	require.NoError(t, err)

	_, err = tc.RunCampInDir("/campaigns/multi-nav-test", "project", "add", "/test/multiproj", "--local", "/test/multiproj")
	require.NoError(t, err)

	// Navigate to projects
	output1, err := tc.RunCampInDir("/campaigns/multi-nav-test", "go", "p", "--print")
	require.NoError(t, err)
	assert.Contains(t, output1, "projects")

	// Navigate back to root with --root flag
	_, err = tc.RunCampInDir("/campaigns/multi-nav-test", "go", "--root", "--print")
	require.NoError(t, err)

	// go without args should now return root (since --root doesn't save as last location)
	output2, err := tc.RunCampInDir("/campaigns/multi-nav-test", "go", "--print")
	require.NoError(t, err)
	assert.Contains(t, output2, "multi-nav-test", "go without args after --root should return root")
}
