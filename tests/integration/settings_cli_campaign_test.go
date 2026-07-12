//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_SettingsCLICampaignScalarWrite exercises the non-interactive
// settings twin end to end: it writes campaign.yaml scalars via
// `camp settings set local.campaign.*` and reads them back. This shares the
// SaveCampaignConfig write path with the interactive TUI but needs no pty, so it
// is a reliable containerized check of the campaign.yaml write.
func TestIntegration_SettingsCLICampaignScalarWrite(t *testing.T) {
	tc := GetSharedContainer(t)

	const (
		campaignDir = "/test/settings-cli-campaign"
		campaignYML = campaignDir + "/.campaign/campaign.yaml"
	)

	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "CLI Twin",
		"--type", "product",
		"-d", "CLI twin integration test",
		"-m", "original mission",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	out, err := tc.RunCampInDir(campaignDir, "settings", "set", "local.campaign.type", "research")
	require.NoError(t, err, "set type; output:\n%s", out)
	out, err = tc.RunCampInDir(campaignDir, "settings", "set", "local.campaign.mission", "Ship the settings TUI")
	require.NoError(t, err, "set mission; output:\n%s", out)

	manifest, err := tc.ReadFile(campaignYML)
	require.NoError(t, err, "campaign.yaml should be readable")
	assert.Contains(t, manifest, "type: research", "type edit should persist")
	assert.Contains(t, manifest, "Ship the settings TUI", "mission edit should persist")
	assert.Contains(t, manifest, "name: CLI Twin", "name should be preserved")

	// The twin's get reflects the written value.
	got, err := tc.RunCampInDir(campaignDir, "settings", "get", "local.campaign.type")
	require.NoError(t, err, "get type; output:\n%s", got)
	assert.Contains(t, got, "research", "get should return the written type")

	// An invalid type is rejected without writing.
	_, setErr := tc.RunCampInDir(campaignDir, "settings", "set", "local.campaign.type", "bogus")
	require.Error(t, setErr, "invalid campaign type should be rejected")
}
