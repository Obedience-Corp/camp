//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit_BasicCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Initialize a basic campaign
	output, err := tc.InitCampaign("/campaigns/test-basic", "test-basic", "")
	require.NoError(t, err, "camp init should succeed")
	assert.Contains(t, output, "Campaign Initialized", "output should confirm initialization")

	// Verify .campaign directory was created
	exists, err := tc.CheckDirExists("/campaigns/test-basic/.campaign")
	require.NoError(t, err)
	assert.True(t, exists, ".campaign directory should exist")

	// Verify campaign.yaml was created
	exists, err = tc.CheckFileExists("/campaigns/test-basic/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.True(t, exists, "campaign.yaml should exist")
}

func TestInit_WithType(t *testing.T) {
	tc := GetSharedContainer(t)

	// Initialize campaign with type
	output, err := tc.InitCampaign("/campaigns/test-product", "test-product", "product")
	require.NoError(t, err, "camp init with type should succeed")
	assert.Contains(t, output, "Campaign Initialized")

	// Verify config has correct type
	content, err := tc.ReadFile("/campaigns/test-product/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.Contains(t, content, "type: product", "campaign type should be product")
}

func TestInit_AllTypes(t *testing.T) {
	tc := GetSharedContainer(t)

	types := []string{"product", "research", "tools", "personal"}

	for _, campType := range types {
		t.Run(campType, func(t *testing.T) {
			path := "/campaigns/test-" + campType
			output, err := tc.InitCampaign(path, "test-"+campType, campType)
			require.NoError(t, err, "camp init with type %s should succeed", campType)
			assert.Contains(t, output, "Campaign Initialized")

			content, err := tc.ReadFile(path + "/.campaign/campaign.yaml")
			require.NoError(t, err)
			assert.Contains(t, content, "type: "+campType)
		})
	}
}

func TestInit_AlreadyExists(t *testing.T) {
	tc := GetSharedContainer(t)

	// Initialize first campaign
	_, err := tc.InitCampaign("/campaigns/test-exists", "test-exists", "")
	require.NoError(t, err)

	// Try to initialize again - should fail (already inside a campaign)
	output, err := tc.InitCampaign("/campaigns/test-exists", "test-exists-2", "")
	require.Error(t, err, "camp init should fail when campaign already exists")
	assert.Contains(t, strings.ToLower(output), "already", "error should mention already inside/exists")
}

func TestInit_NestedCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	// Initialize parent campaign
	_, err := tc.InitCampaign("/campaigns/test-parent", "test-parent", "")
	require.NoError(t, err)

	// Create nested directory first so camp can detect we're inside a campaign
	_, _, err = tc.ExecCommand("mkdir", "-p", "/campaigns/test-parent/nested")
	require.NoError(t, err)

	// Try to initialize nested campaign - should fail because we're inside existing campaign
	output, err := tc.InitCampaign("/campaigns/test-parent/nested", "test-nested", "")
	require.Error(t, err, "camp init should fail inside existing campaign")
	assert.Contains(t, strings.ToLower(output), "inside", "error should mention being inside a campaign")
}

func TestInit_DirectoryStructure(t *testing.T) {
	tc := GetSharedContainer(t)

	// Initialize campaign
	_, err := tc.InitCampaign("/campaigns/test-structure", "test-structure", "product")
	require.NoError(t, err)

	// Verify expected directories
	expectedDirs := []string{
		"/campaigns/test-structure/.campaign",
		"/campaigns/test-structure/projects",
	}

	for _, dir := range expectedDirs {
		exists, err := tc.CheckDirExists(dir)
		require.NoError(t, err, "checking dir %s", dir)
		assert.True(t, exists, "directory %s should exist", dir)
	}

	// Verify campaign.yaml exists
	exists, err := tc.CheckFileExists("/campaigns/test-structure/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.True(t, exists, "campaign.yaml should exist")
}

func TestInit_CampaignYAMLContent(t *testing.T) {
	tc := GetSharedContainer(t)

	// Initialize campaign
	_, err := tc.InitCampaign("/campaigns/test-yaml", "my-test-campaign", "research")
	require.NoError(t, err)

	// Read and verify config content
	content, err := tc.ReadFile("/campaigns/test-yaml/.campaign/campaign.yaml")
	require.NoError(t, err)

	assert.Contains(t, content, "name: my-test-campaign", "config should contain name")
	assert.Contains(t, content, "type: research", "config should contain type")
}

func TestInit_InvalidType(t *testing.T) {
	tc := GetSharedContainer(t)

	// Try to initialize with invalid type
	output, err := tc.RunCamp("init", "/campaigns/test-invalid", "--name", "test-invalid", "--type", "invalid-type")
	require.Error(t, err, "camp init should fail with invalid type")
	assert.Contains(t, strings.ToLower(output), "invalid", "error should mention invalid type")
}

func TestInit_MissingName(t *testing.T) {
	tc := GetSharedContainer(t)

	// Try to initialize without name - should use directory name
	output, err := tc.RunCamp("init", "/campaigns/auto-named-campaign")
	require.NoError(t, err, "camp init without name should succeed (uses dir name)")
	assert.Contains(t, output, "Campaign Initialized")

	// Verify config uses directory name
	content, err := tc.ReadFile("/campaigns/auto-named-campaign/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.Contains(t, content, "name: auto-named-campaign")
}
