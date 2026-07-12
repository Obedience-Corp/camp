//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_SettingsRegistrySafeEdit drives `camp settings` through a real
// TTY to the global registry surface, edits a campaign's org, and verifies the
// change is persisted to registry.json via the registry API (version and other
// fields intact, UUID key unchanged). The registry lives at a container path
// pointed to by CAMP_REGISTRY_PATH, so the test is fully isolated.
func TestIntegration_SettingsRegistrySafeEdit(t *testing.T) {
	skipUnlessSettingsTTYTests(t)
	tc := GetSharedContainer(t)

	const (
		registryPath = "/test/settings-registry.json"
		campaignUUID = "11111111-1111-1111-1111-111111111111"
	)

	initialRegistry := `{
  "version": 2,
  "campaigns": {
    "11111111-1111-1111-1111-111111111111": {
      "name": "Alpha",
      "path": "/test/alpha",
      "org": "personal"
    }
  }
}
`
	require.NoError(t, tc.WriteFile(registryPath, initialRegistry))

	// Reach the registry, pick the campaign, then edit the Org field:
	//   \r        submit Display name (unchanged), advance to Org
	//   \x15obc   clear Org and type the new value
	//   \r\r      advance past Path (unchanged) and submit the form
	editOrg := "\r\x15obc\r\r"
	output, err := tc.RunCampInteractiveStepsInDirWithEnv(
		"/test",
		map[string]string{"CAMP_REGISTRY_PATH": registryPath},
		[]InteractiveStep{
			{WaitFor: "Select configuration scope", Input: "\r"},   // top: Global
			{WaitFor: "Files under", Input: "\r"},                  // global: Campaign registry (first row)
			{WaitFor: "Alpha", Input: "\r"},                        // registry picker: select Alpha
			{WaitFor: "Display name", Input: editOrg},              // edit form: change Org, submit
			{WaitFor: "", Input: ""},                               // settle: let the form save and the picker reload
			{WaitFor: "", Input: "\x03"},                           // picker is showing; abort back to the global menu
			{WaitFor: "Files under", Input: "\x03"},                // global menu; abort back to the top menu
			{WaitFor: "Select configuration scope", Input: "\x03"}, // top menu; exit
		},
		"--no-color", "settings",
	)
	require.NoError(t, err, "interactive registry edit should succeed; output:\n%s", output)

	saved, err := tc.ReadFile(registryPath)
	require.NoError(t, err, "registry.json should be readable after edit")

	assert.Contains(t, saved, `"org": "obc"`, "org edit should persist")
	assert.Contains(t, saved, `"name": "Alpha"`, "name should be preserved")
	assert.Contains(t, saved, `"path": "/test/alpha"`, "path should be preserved")
	assert.Contains(t, saved, `"version": 2`, "registry version should stay 2")
	assert.Contains(t, saved, campaignUUID, "UUID key should be unchanged")
}
