//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCampSettings_CampaignsDirEditAndClear(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.Shell(t, "rm -rf /root/.obey/campaign")

	backFromGlobalSettings := strings.Repeat("\x1b[B", 6) + "\r"
	exitCampSettings := "\x1b[B\r"
	openCampaignsDir := strings.Repeat("\x1b[B", 2) + "\r"

	output, err := tc.RunCampInteractiveStepsInDir(
		"/test",
		[]InteractiveStep{
			{WaitFor: "Camp Settings", Input: "\r"},
			{WaitFor: "Changes apply to all campaigns", Input: openCampaignsDir},
			{WaitFor: "Where 'camp create' places new campaigns", Input: "/tmp/settings-campaigns\r"},
			{WaitFor: "/tmp/settings-campaigns", Input: backFromGlobalSettings},
			{WaitFor: "Select configuration scope", Input: exitCampSettings},
		},
		"settings",
	)
	require.NoError(t, err, "camp settings should save Campaigns Dir; output:\n%s", output)
	assert.Contains(t, output, "Campaigns Dir")

	configJSON, err := tc.ReadFile("/root/.obey/campaign/config.json")
	require.NoError(t, err)
	assert.Contains(t, configJSON, `"campaigns_dir": "/tmp/settings-campaigns"`)

	clearOutput, err := tc.RunCampInteractiveStepsInDir(
		"/test",
		[]InteractiveStep{
			{WaitFor: "Camp Settings", Input: "\r"},
			{WaitFor: "/tmp/settings-campaigns", Input: openCampaignsDir},
			{WaitFor: "Where 'camp create' places new campaigns", Input: "\x15\r"},
			{WaitFor: "~/campaigns", Input: backFromGlobalSettings},
			{WaitFor: "Select configuration scope", Input: exitCampSettings},
		},
		"settings",
	)
	require.NoError(t, err, "camp settings should clear Campaigns Dir; output:\n%s", clearOutput)

	configJSON, err = tc.ReadFile("/root/.obey/campaign/config.json")
	require.NoError(t, err)
	assert.NotContains(t, configJSON, "campaigns_dir")
}
