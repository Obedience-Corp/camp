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

// TestInit_FestivalOwnership verifies that default init creates festivals/
// when the fest binary is available.
func TestInit_FestivalOwnership(t *testing.T) {
	t.Run("festivals exists when fest available", func(t *testing.T) {
		if !festAvailable {
			t.Skip("fest binary not available in container; skipping festival-present sub-test")
		}

		tc := GetSharedContainer(t)
		path := "/campaigns/init-with-fest"

		output, err := tc.RunCamp("init", path,
			"--name", "init-with-fest",
			"-d", "desc",
			"-m", "mission",
			"--no-git",
		)
		require.NoError(t, err, "camp init should succeed; output: %s", output)

		exists, checkErr := tc.CheckDirExists(path + "/festivals")
		require.NoError(t, checkErr)
		assert.True(t, exists, "festivals/ should exist when fest is available")

		markers := []string{".festival", "fest.yaml", ".fest"}
		count := 0
		for _, marker := range markers {
			if exists, _ := tc.CheckDirExists(path + "/festivals/" + marker); exists {
				count++
			}
			if exists, _ := tc.CheckFileExists(path + "/festivals/" + marker); exists {
				count++
			}
		}
		assert.GreaterOrEqual(t, count, 1,
			"festivals/ should contain at least one fest initialization marker")
	})
}

func TestInit_WorkflowExploreScaffoldAndShortcut(t *testing.T) {
	tc := GetSharedContainer(t)

	_, err := tc.InitCampaign("/campaigns/test-explore", "test-explore", "product")
	require.NoError(t, err)

	// workflow/explore should be scaffolded.
	exists, err := tc.CheckDirExists("/campaigns/test-explore/workflow/explore")
	require.NoError(t, err)
	assert.True(t, exists, "workflow/explore should exist in new campaign scaffold")

	// workflow/explore guidance should differentiate it from workflow/design.
	exploreObey, err := tc.ReadFile("/campaigns/test-explore/workflow/explore/OBEY.md")
	require.NoError(t, err)
	assert.Contains(t, exploreObey, "workflow/design", "explore guidance should reference design differentiation")

	// jumps defaults should include the ex shortcut.
	jumps, err := tc.ReadFile("/campaigns/test-explore/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	assert.Contains(t, jumps, "ex:", "jumps config should include ex shortcut")
	assert.Contains(t, jumps, "workflow/explore/", "ex shortcut should target workflow/explore/")

	// Shortcut should navigate to workflow/explore path.
	output, err := tc.RunCampInDir("/campaigns/test-explore", "go", "ex", "--print")
	require.NoError(t, err)
	assert.Contains(t, output, "/campaigns/test-explore/workflow/explore")
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
	output, err := tc.RunCamp("init", "/campaigns/test-invalid", "--name", "test-invalid", "-d", "Test", "-m", "Test", "--type", "invalid-type")
	require.Error(t, err, "camp init should fail with invalid type")
	assert.Contains(t, strings.ToLower(output), "invalid", "error should mention invalid type")
}

func TestInit_MissingName(t *testing.T) {
	tc := GetSharedContainer(t)

	// Try to initialize without name - should use directory name
	output, err := tc.RunCamp("init", "/campaigns/auto-named-campaign", "-d", "Test", "-m", "Test")
	require.NoError(t, err, "camp init without name should succeed (uses dir name)")
	assert.Contains(t, output, "Campaign Initialized")

	// Verify config uses directory name
	content, err := tc.ReadFile("/campaigns/auto-named-campaign/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.Contains(t, content, "name: auto-named-campaign")
}
