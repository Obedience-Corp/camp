//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_SettingsConceptsEditorRoundTrip drives `camp settings` through
// a real TTY to the campaign-manifest concepts editor (in-TUI YAML text field),
// pastes a known-valid concept list, and verifies the edit is persisted to
// .campaign/campaign.yaml while the rest of the manifest is left intact.
func TestIntegration_SettingsConceptsEditorRoundTrip(t *testing.T) {
	skipUnlessSettingsTTYTests(t)
	tc := GetSharedContainer(t)

	const (
		campaignDir = "/test/settings-concepts"
		campaignYML = campaignDir + "/.campaign/campaign.yaml"
	)

	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Concepts Round Trip",
		"--type", "product",
		"-d", "Settings concepts integration test",
		"-m", "Verify campaign.yaml concepts round-trip",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	// huh Text: type replacement YAML, then Ctrl+D / submit per form binding.
	// Interactive harness uses Enter to submit multi-line when WaitFor matches
	// the text field title, then aborts menus with Ctrl+C.
	conceptYAML := "- name: integration-concept\n  path: some/integration/path/\n  description: added by integration test\n"
	steps := []InteractiveStep{
		{WaitFor: "Select configuration scope", Input: "\x1b[B\r"}, // top: Local
		{WaitFor: "Files under .campaign/", Input: "\r"},           // Campaign manifest
		{WaitFor: "Concepts taxonomy", Input: "\x1b[B\x1b[B\r"},    // Concepts row
		{WaitFor: "Concepts taxonomy (YAML)", Input: "\x01\x0b" + conceptYAML + "\r"}, // clear-ish + paste + submit
		{WaitFor: "Concepts taxonomy", Input: "\x03"},              // back at manifest
		{WaitFor: "Files under .campaign/", Input: "\x03"},         // local
		{WaitFor: "Select configuration scope", Input: "\x03"},     // top exit
	}
	output, err := tc.RunCampInteractiveStepsInDir(
		campaignDir,
		steps,
		"--no-color", "settings",
	)
	require.NoError(t, err, "interactive settings flow should succeed; output:\n%s", output)

	manifest, err := tc.ReadFile(campaignYML)
	require.NoError(t, err, "campaign.yaml should be readable after edit")

	assert.Contains(t, manifest, "integration-concept", "edited concept name should persist to campaign.yaml")
	assert.Contains(t, manifest, "some/integration/path/", "edited concept path should persist to campaign.yaml")
	assert.Contains(t, manifest, "name: Concepts Round Trip", "campaign name should be preserved through the concepts edit")
}
