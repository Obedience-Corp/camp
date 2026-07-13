//go:build integration
// +build integration

package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_SettingsConceptsEditorRoundTrip drives `camp settings` through
// a real TTY to the campaign-manifest concepts editor (in-TUI YAML text field),
// enters a known-valid concept entry, and verifies it is persisted to
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

	// Input encoding for the concepts editor (huh Text over a bubbles textarea):
	//   - "\n" (0x0a = Ctrl+J) inserts a newline INTO the textarea; plain
	//     "\r" (Enter) is the field's submit key, not a newline.
	//   - "\x01\x0b" is Ctrl+A then Ctrl+K: move to line start, kill to end of
	//     line. This is a partial in-place edit, NOT a whole-buffer clear, so
	//     the seeded concepts remain and the new entry is appended. The test
	//     therefore asserts presence of the new concept, not a clean replace.
	//   - "\x03" is Ctrl+C, used to back out of each menu level.
	// The external-editor escape hatch (Ctrl+E) is disabled on this field, so
	// the flow stays entirely in-TUI.
	conceptYAML := "- name: integration-concept\n  path: some/integration/path/\n"
	steps := []InteractiveStep{
		{WaitFor: "Select configuration scope", Input: "\x1b[B\r"},                    // top: Local
		{WaitFor: "Files under .campaign/", Input: "\r"},                              // Campaign manifest
		{WaitFor: "Concepts taxonomy", Input: "\x1b[B\x1b[B\r"},                       // Concepts row
		{WaitFor: "Concepts taxonomy (YAML)", Input: "\x01\x0b" + conceptYAML + "\r"}, // edit + append + submit
		{WaitFor: "Concepts taxonomy", Input: "\x03"},                                 // back at manifest
		{WaitFor: "Files under .campaign/", Input: "\x03"},                            // local
		{WaitFor: "Select configuration scope", Input: "\x03"},                        // top exit
	}
	// Deep multi-screen flow with a multiline paste: give it more than the
	// default per-session budget so per-character typing plus camp's TUI init
	// latency cannot trip the deadline mid-run.
	output, err := tc.RunCampInteractiveStepsInDirTimeout(
		campaignDir,
		60*time.Second,
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
