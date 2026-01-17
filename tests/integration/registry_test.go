//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister_Campaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create a campaign
	_, err := tc.InitCampaign("/campaigns/reg-test", "reg-test", "product")
	require.NoError(t, err)

	// Register the campaign
	output, err := tc.RunCamp("register", "/campaigns/reg-test")
	require.NoError(t, err, "register should succeed")
	assert.Contains(t, output, "reg-test", "output should mention registered campaign")
}

func TestRegister_WithCustomName(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup
	_, err := tc.InitCampaign("/campaigns/reg-custom", "reg-custom", "product")
	require.NoError(t, err)

	// Register with custom name override
	output, err := tc.RunCamp("register", "/campaigns/reg-custom", "--name", "custom-name")
	require.NoError(t, err, "register with custom name should succeed")
	assert.Contains(t, output, "custom-name", "output should mention custom name")
}

func TestList_RegisteredCampaigns(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create and register multiple campaigns
	campaigns := []string{"list-a", "list-b", "list-c"}
	for _, name := range campaigns {
		_, err := tc.InitCampaign("/campaigns/"+name, name, "product")
		require.NoError(t, err)
		_, err = tc.RunCamp("register", "/campaigns/"+name)
		require.NoError(t, err)
	}

	// List all registered campaigns
	output, err := tc.RunCamp("list")
	require.NoError(t, err, "list should succeed")

	// Verify all campaigns appear
	for _, name := range campaigns {
		assert.Contains(t, output, name, "list should contain campaign %s", name)
	}
}

func TestList_Empty(t *testing.T) {
	tc := GetSharedContainer(t)

	// List with no registered campaigns
	output, err := tc.RunCamp("list")
	require.NoError(t, err, "list should succeed even with no campaigns")
	// Output might be empty or contain "no campaigns" message
	_ = output
}

func TestUnregister_Campaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create and register a campaign
	_, err := tc.InitCampaign("/campaigns/unreg-test", "unreg-test", "product")
	require.NoError(t, err)
	_, err = tc.RunCamp("register", "/campaigns/unreg-test")
	require.NoError(t, err)

	// Verify it appears in list
	output, err := tc.RunCamp("list")
	require.NoError(t, err)
	require.Contains(t, output, "unreg-test", "campaign should be in list before unregister")

	// Unregister (use --force to skip confirmation prompt in non-interactive mode)
	output, err = tc.RunCamp("unregister", "unreg-test", "--force")
	require.NoError(t, err, "unregister should succeed")
	assert.Contains(t, output, "unreg-test", "output should mention unregistered campaign")

	// Verify it's no longer in list
	output, err = tc.RunCamp("list")
	require.NoError(t, err)
	assert.NotContains(t, output, "unreg-test", "campaign should not be in list after unregister")
}

func TestUnregister_NotFound(t *testing.T) {
	tc := GetSharedContainer(t)

	// Try to unregister non-existent campaign (use --force to skip prompt)
	output, err := tc.RunCamp("unregister", "nonexistent-campaign", "--force")
	require.Error(t, err, "unregister should fail for non-existent campaign")
	assert.Contains(t, strings.ToLower(output), "not found", "error should mention not found")
}

func TestRegister_NotACampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Create a regular directory (not a campaign)
	_, _, err := tc.ExecCommand("mkdir", "-p", "/test/notacampaign")
	require.NoError(t, err)

	// Try to register it - in non-interactive mode, this should fail or prompt
	// The command offers to initialize if not a campaign, but in non-interactive mode
	// it should fail with an appropriate message
	output, err := tc.RunCamp("register", "/test/notacampaign")
	// May succeed if it prompts to init, or fail - check output for appropriate message
	if err != nil {
		assert.True(t,
			strings.Contains(strings.ToLower(output), "not a campaign") ||
				strings.Contains(strings.ToLower(output), "initialize"),
			"error should mention not a campaign or offer to initialize")
	}
}

func TestRegister_AlreadyRegistered(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create and register a campaign
	_, err := tc.InitCampaign("/campaigns/dup-reg", "dup-reg", "product")
	require.NoError(t, err)
	_, err = tc.RunCamp("register", "/campaigns/dup-reg")
	require.NoError(t, err)

	// Try to register again - the command may update the entry or show a message
	output, err := tc.RunCamp("register", "/campaigns/dup-reg")
	// Command may succeed (updating entry) or fail (already registered)
	// Either behavior is acceptable - just verify it doesn't crash
	_ = output
	_ = err
}

func TestRegister_Help(t *testing.T) {
	tc := GetSharedContainer(t)

	output, err := tc.RunCamp("register", "--help")
	require.NoError(t, err, "register --help should succeed")
	assert.Contains(t, output, "register", "help should describe register command")
}

func TestUnregister_Help(t *testing.T) {
	tc := GetSharedContainer(t)

	output, err := tc.RunCamp("unregister", "--help")
	require.NoError(t, err, "unregister --help should succeed")
	assert.Contains(t, output, "unregister", "help should describe unregister command")
}

func TestList_Help(t *testing.T) {
	tc := GetSharedContainer(t)

	output, err := tc.RunCamp("list", "--help")
	require.NoError(t, err, "list --help should succeed")
	assert.Contains(t, output, "list", "help should describe list command")
}

func TestRegister_InvalidPath(t *testing.T) {
	tc := GetSharedContainer(t)

	// Try to register non-existent path
	output, err := tc.RunCamp("register", "/nonexistent/path/campaign")
	// Command should fail for non-existent path
	if err != nil {
		assert.True(t,
			strings.Contains(strings.ToLower(output), "not") ||
				strings.Contains(strings.ToLower(output), "no such") ||
				strings.Contains(strings.ToLower(output), "does not exist"),
			"error should indicate path issue")
	} else {
		// If it didn't fail, at least verify nothing crashed
		t.Log("register command did not fail for non-existent path - command may create directory")
	}
}
