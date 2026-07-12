//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_SettingsAllowlistToggle drives camp settings to the command
// allowlist and toggles a command off, then verifies allowlist.json (the command
// is no longer allowed, inherit_defaults intact). Gated like the other settings
// TUI tests because of the tracked bubbletea init tty-query stall.
func TestIntegration_SettingsAllowlistToggle(t *testing.T) {
	skipUnlessSettingsTTYTests(t)
	tc := GetSharedContainer(t)

	const (
		campaignDir   = "/test/settings-allowlist"
		allowlistJSON = campaignDir + "/.campaign/settings/allowlist.json"
	)

	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Allowlist Test",
		"--type", "product",
		"-d", "Allowlist integration test",
		"-m", "Verify allowlist toggle",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	// Scaffolded commands are camp, fest, git, just (sorted). git is index 2 and
	// starts allowed; toggling opens its sub-menu with "Set not allowed" first.
	steps := []InteractiveStep{
		{WaitFor: "Select configuration scope", Input: "\x1b[B\r"},   // top: Local
		{WaitFor: "Files under .campaign/", Input: "\x1b[B\x1b[B\r"}, // local: Command allowlist (3rd row)
		{WaitFor: "inherit_defaults", Input: "\x1b[B\x1b[B\r"},       // allowlist: select git (3rd command)
		{WaitFor: "Remove command", Input: "\r"},                     // command sub-menu: Set not allowed
		{WaitFor: "", Input: ""},                                     // settle: save and reload
		{WaitFor: "", Input: "\x03"},                                 // allowlist menu; abort
		{WaitFor: "Files under .campaign/", Input: "\x03"},           // local menu; abort
		{WaitFor: "Select configuration scope", Input: "\x03"},       // top menu; exit
	}
	output, err := tc.RunCampInteractiveStepsInDir(campaignDir, steps, "--no-color", "settings")
	require.NoError(t, err, "interactive allowlist toggle should succeed; output:\n%s", output)

	saved, err := tc.ReadFile(allowlistJSON)
	require.NoError(t, err, "allowlist.json should be readable")
	assert.Contains(t, saved, `"git"`, "git entry should still exist")
	assert.Contains(t, saved, `"allowed": false`, "a command should now be disallowed")
	assert.Contains(t, saved, `"inherit_defaults": true`, "inherit_defaults should be unchanged")
}
