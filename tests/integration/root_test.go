//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type campaignRootJSON struct {
	RelativeRoot string `json:"relative_root"`
	CWD          string `json:"cwd"`
	AbsoluteRoot string `json:"absolute_root"`
}

func TestRoot_AtCampaignRoot(t *testing.T) {
	tc := GetSharedContainer(t)

	_, err := tc.InitCampaign("/campaigns/root-cmd", "root-cmd", "product")
	require.NoError(t, err)

	output, err := tc.RunCampInDir("/campaigns/root-cmd", "root")
	require.NoError(t, err, "camp root should succeed at the campaign root")
	assert.Equal(t, ".", strings.TrimSpace(output))
}

func TestRoot_FromNestedDirectory(t *testing.T) {
	tc := GetSharedContainer(t)

	_, err := tc.InitCampaign("/campaigns/root-nested", "root-nested", "product")
	require.NoError(t, err)

	_, exitCode, err := tc.ExecCommand("mkdir", "-p", "/campaigns/root-nested/docs/notes")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	output, err := tc.RunCampInDir("/campaigns/root-nested/docs/notes", "root")
	require.NoError(t, err, "camp root should succeed from nested directories")
	assert.Equal(t, "../..", strings.TrimSpace(output))
}

func TestRoot_JSON(t *testing.T) {
	tc := GetSharedContainer(t)

	_, err := tc.InitCampaign("/campaigns/root-json", "root-json", "product")
	require.NoError(t, err)

	_, exitCode, err := tc.ExecCommand("mkdir", "-p", "/campaigns/root-json/projects/app")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	output, err := tc.RunCampInDir("/campaigns/root-json/projects/app", "root", "--json")
	require.NoError(t, err, "camp root --json should succeed")

	var got campaignRootJSON
	require.NoError(t, json.Unmarshal([]byte(output), &got), "json output should parse")

	assert.Equal(t, "../..", got.RelativeRoot)
	assert.Equal(t, "/campaigns/root-json/projects/app", got.CWD)
	assert.Equal(t, "/campaigns/root-json", got.AbsoluteRoot)
}

func TestRoot_NotInCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	output, err := tc.RunCampInDir("/test", "root")
	require.Error(t, err, "camp root should fail outside a campaign")
	assert.Contains(t, strings.ToLower(output), "campaign", "error should mention campaign detection")
}

func TestRoot_RespectsCampRootOverrideOutsideCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	_, err := tc.InitCampaign("/campaigns/root-env", "root-env", "product")
	require.NoError(t, err)

	output, exitCode, err := tc.ExecCommand(
		"sh", "-c",
		"cd /test && CAMP_ROOT=/campaigns/root-env /camp root 2>&1",
	)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "camp root should honor CAMP_ROOT from outside the campaign: %s", output)
	assert.Equal(t, "../campaigns/root-env", strings.TrimSpace(output))
}
