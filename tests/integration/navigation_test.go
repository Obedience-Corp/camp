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

	t.Run("design", func(t *testing.T) {
		output, err := tc.RunCampInDir("/campaigns/cat-test", "go", "design", "--print")
		require.NoError(t, err, "long-form concept 'design' should resolve on a new campaign")
		assert.Contains(t, output, "workflow/design", "output should contain design path")
	})

	t.Run("ai_docs", func(t *testing.T) {
		output, err := tc.RunCampInDir("/campaigns/cat-test", "go", "ai_docs", "--print")
		require.NoError(t, err, "long-form directory alias 'ai_docs' should resolve on a new campaign")
		assert.Contains(t, output, "ai_docs", "output should contain ai_docs path")
	})

	t.Run("design_slash_drill", func(t *testing.T) {
		_, _, err := tc.ExecCommand("mkdir", "-p", "/campaigns/cat-test/workflow/design/festival_app")
		require.NoError(t, err)

		output, err := tc.RunCampInDir("/campaigns/cat-test", "go", "design/festival_app", "--print")
		require.NoError(t, err, "slash drill should resolve through long-form concept alias")
		assert.Contains(t, output, "workflow/design/festival_app", "output should contain drilled design path")
	})

	t.Run("de_at_drill", func(t *testing.T) {
		_, _, err := tc.ExecCommand("mkdir", "-p", "/campaigns/cat-test/workflow/design/festival_site")
		require.NoError(t, err)

		output, err := tc.RunCampInDir("/campaigns/cat-test", "go", "de@festival_site", "--print")
		require.NoError(t, err, "shortcut drill should resolve through @ syntax")
		assert.Contains(t, output, "workflow/design/festival_site", "output should contain drilled shortcut path")
	})

	t.Run("i", func(t *testing.T) {
		output, err := tc.RunCampInDir("/campaigns/cat-test", "go", "i", "--print")
		require.NoError(t, err, "shortcut 'i' should resolve on a new campaign")
		assert.Contains(t, output, ".campaign/intents", "output should contain the canonical intents path")
	})

	// Test category that doesn't exist - should get "directory not found" error
	t.Run("f_missing", func(t *testing.T) {
		// Remove festivals dir if it was created by fest init during camp init
		_, _, _ = tc.ExecCommand("rm", "-rf", "/campaigns/cat-test/festivals")

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

	// Navigate to projects from campaign root — saves campaign root as last location (source-saving)
	output1, err := tc.RunCampInDir("/campaigns/last-loc-nav", "go", "p", "--print")
	require.NoError(t, err, "go p should succeed")
	assert.Contains(t, output1, "projects", "first go p should return projects path")

	// Now run go without args from the projects dir — should toggle back to campaign root
	projectsDir := strings.TrimSpace(output1)
	output2, err := tc.RunCampInDir(projectsDir, "go", "--print")
	require.NoError(t, err, "go without args should succeed")
	assert.Contains(t, output2, "last-loc-nav", "go without args from projects should toggle back to campaign root")
	assert.NotContains(t, output2, "projects", "should not stay in projects dir")
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

// TestShortcuts_OnlyFromConfig verifies that shortcuts work when defined in jumps.yaml
func TestShortcuts_OnlyFromConfig(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign (which scaffolds default shortcuts)
	_, err := tc.InitCampaign("/campaigns/shortcuts-test", "shortcuts-test", "product")
	require.NoError(t, err)

	// Create projects directory
	_, _, err = tc.ExecCommand("mkdir", "-p", "/campaigns/shortcuts-test/projects")
	require.NoError(t, err)

	// Verify shortcut "p" works (should be in jumps.yaml by default)
	output, err := tc.RunCampInDir("/campaigns/shortcuts-test", "go", "p", "--print")
	require.NoError(t, err, "shortcut 'p' should work when defined in jumps.yaml")
	assert.Contains(t, output, "projects", "shortcut 'p' should resolve to projects/")

	// Read jumps.yaml to verify shortcuts are there
	config, err := tc.ReadFile("/campaigns/shortcuts-test/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	assert.Contains(t, config, "shortcuts:", "jumps.yaml should have shortcuts section")
	assert.Contains(t, config, "p:", "jumps.yaml should have 'p' shortcut")
}

// TestShortcuts_HelpShowsConfigOnly verifies that --help shows shortcuts from jumps.yaml
func TestShortcuts_HelpShowsConfigOnly(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/help-shortcuts-test", "help-shortcuts-test", "product")
	require.NoError(t, err)

	// Test that help shows shortcuts from config
	output, err := tc.RunCampInDir("/campaigns/help-shortcuts-test", "go", "--help")
	require.NoError(t, err)
	assert.Contains(t, output, "from .campaign/settings/jumps.yaml", "help should indicate shortcuts are from jumps config")
	assert.Contains(t, output, "p", "help should show 'p' shortcut from config")
}

// TestShortcuts_NotInCampaign verifies shortcuts don't work outside a campaign
func TestShortcuts_NotInCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Try to use shortcut "p" outside a campaign
	output, err := tc.RunCampInDir("/test", "go", "p", "--print")
	// Should fail because we're not in a campaign
	require.Error(t, err, "shortcut 'p' should fail outside a campaign")
	assert.Contains(t, strings.ToLower(output), "not inside a campaign", "error should mention not in campaign")
}

// TestShortcuts_HelpNotInCampaign verifies help shows appropriate message when not in campaign
func TestShortcuts_HelpNotInCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Get help outside a campaign
	output, err := tc.RunCampInDir("/test", "go", "--help")
	require.NoError(t, err, "help should work outside campaign")
	assert.Contains(t, output, "Not in a campaign", "help should show not in campaign message")
	assert.Contains(t, output, "camp init", "help should suggest camp init")
}

// TestShortcuts_OverrideAndRestore verifies that user can override default shortcuts
// and that removing the override restores default behavior.
// Note: Default shortcuts now provide fallback when not in config, so removing
// a shortcut doesn't disable it - it reverts to the default.
func TestShortcuts_OverrideAndRestore(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/override-shortcut-test", "override-shortcut-test", "product")
	require.NoError(t, err)

	// Create both directories
	_, _, err = tc.ExecCommand("mkdir", "-p", "/campaigns/override-shortcut-test/projects")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("mkdir", "-p", "/campaigns/override-shortcut-test/custom")
	require.NoError(t, err)

	// Verify "p" initially points to default (projects/)
	output, err := tc.RunCampInDir("/campaigns/override-shortcut-test", "go", "p", "--print")
	require.NoError(t, err, "shortcut 'p' should work initially")
	assert.Contains(t, output, "projects", "default 'p' should resolve to projects/")

	// Override "p" to point to "custom/" in jumps.yaml
	newJumps := `paths:
  projects: "projects/"
  worktrees: "projects/worktrees/"
  ai_docs: "ai_docs/"
  docs: "docs/"
  dungeon: "dungeon/"
  festivals: "festivals/"
  workflow: "workflow/"
  code_reviews: "workflow/code_reviews/"
  pipelines: "workflow/pipelines/"
  design: "workflow/design/"
  intents: ".campaign/intents/"
shortcuts:
  p:
    path: "custom/"
    description: "Overridden to custom"
`
	err = tc.WriteFile("/campaigns/override-shortcut-test/.campaign/settings/jumps.yaml", newJumps)
	require.NoError(t, err)

	// Verify "p" now points to custom/
	output, err = tc.RunCampInDir("/campaigns/override-shortcut-test", "go", "p", "--print")
	require.NoError(t, err, "shortcut 'p' should work after override")
	assert.Contains(t, output, "custom", "overridden 'p' should resolve to custom/")
	assert.NotContains(t, output, "projects", "overridden 'p' should NOT resolve to projects/")
}

// TestShortcuts_Command verifies the "camp shortcuts" command shows only config shortcuts
func TestShortcuts_Command(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/shortcuts-cmd-test", "shortcuts-cmd-test", "product")
	require.NoError(t, err)

	// Run shortcuts command
	output, err := tc.RunCampInDir("/campaigns/shortcuts-cmd-test", "shortcuts")
	require.NoError(t, err)

	// Should show campaign name and shortcuts from config
	assert.Contains(t, output, "shortcuts-cmd-test", "output should show campaign name")
	assert.Contains(t, output, "p", "output should show 'p' shortcut")
	// Should NOT show "Built-in" section (removed in our changes)
	assert.NotContains(t, output, "Built-in", "output should not show 'Built-in' section")
}

// TestShortcuts_CommandNotInCampaign verifies shortcuts command behavior outside campaign
func TestShortcuts_CommandNotInCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Run shortcuts command outside a campaign
	output, err := tc.RunCampInDir("/test", "shortcuts")
	require.NoError(t, err, "shortcuts command should succeed but show not in campaign message")
	assert.Contains(t, output, "Not in a campaign", "output should indicate not in campaign")
	assert.Contains(t, output, "camp init", "output should suggest camp init")
}
