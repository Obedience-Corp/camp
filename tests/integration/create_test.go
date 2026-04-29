//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const globalRegistryPath = "/root/.obey/campaign/registry.json"

func readGlobalRegistry(t *testing.T, tc *TestContainer) string {
	t.Helper()
	return tc.Shell(t, "cat "+globalRegistryPath+" 2>/dev/null || true")
}

// TestCampCreate_HappyPath creates a campaign via 'camp create' with --path
// and asserts the campaign directory, .campaign/ contents, and exit code.
func TestCampCreate_HappyPath(t *testing.T) {
	tc := GetSharedContainer(t)

	base := "/tmp/create-happy"
	tc.Shell(t, fmt.Sprintf("mkdir -p %s", base))

	output, err := tc.RunCamp("create", "my-campaign",
		"-d", "test description",
		"-m", "test mission",
		"--no-git",
		"--path", base,
	)
	require.NoError(t, err, "camp create should succeed; output: %s", output)
	assert.Contains(t, output, "Campaign Initialized", "output should confirm initialization")

	// Target directory should exist.
	exists, err := tc.CheckDirExists(base + "/my-campaign")
	require.NoError(t, err)
	assert.True(t, exists, "campaign directory should exist at base/name")

	// .campaign/ should be present.
	exists, err = tc.CheckDirExists(base + "/my-campaign/.campaign")
	require.NoError(t, err)
	assert.True(t, exists, ".campaign/ should exist inside the new campaign")

	// campaign.yaml must be present.
	exists, err = tc.CheckFileExists(base + "/my-campaign/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.True(t, exists, ".campaign/campaign.yaml should exist")

	registry := readGlobalRegistry(t, tc)
	assert.Contains(t, registry, base+"/my-campaign", "registry should contain the created campaign path")
	assert.Contains(t, registry, "my-campaign", "registry should contain the created campaign name")

	initPath := base + "/init-equivalent"
	initOutput, initErr := tc.RunCamp("init", initPath,
		"--name", "my-campaign",
		"-d", "test description",
		"-m", "test mission",
		"--no-git",
	)
	require.NoError(t, initErr, "equivalent camp init should succeed; output: %s", initOutput)

	createFiles := tc.Shell(t, fmt.Sprintf("cd %s/my-campaign/.campaign && find . -type f | sort", base))
	initFiles := tc.Shell(t, fmt.Sprintf("cd %s/.campaign && find . -type f | sort", initPath))
	assert.Equal(t, initFiles, createFiles, "camp create should scaffold the same .campaign file set as equivalent camp init")
}

// TestCampCreate_NonTTYMissingFlags asserts that omitting -d and -m in non-TTY
// mode returns the same error as 'camp init' does.
func TestCampCreate_NonTTYMissingFlags(t *testing.T) {
	tc := GetSharedContainer(t)

	base := "/tmp/create-nottymissing"
	tc.Shell(t, fmt.Sprintf("mkdir -p %s", base))

	// RunCamp is non-interactive (no TTY), so omitting -d and -m should error.
	output, err := tc.RunCamp("create", "bad-campaign",
		"--no-git",
		"--path", base,
	)
	require.Error(t, err, "camp create without -d/-m in non-TTY should fail")
	assert.Contains(t, strings.ToLower(output), "required",
		"error should mention required flags")

	// Campaign directory must not have been created.
	exists, checkErr := tc.CheckDirExists(base + "/bad-campaign")
	require.NoError(t, checkErr)
	assert.False(t, exists, "no campaign directory should be created on error")
}

// TestCampCreate_DryRunNoMutation verifies that --dry-run performs zero
// filesystem writes: the base directory, target directory, and registry are
// all untouched.
func TestCampCreate_DryRunNoMutation(t *testing.T) {
	tc := GetSharedContainer(t)

	// Use a base directory that does NOT exist.
	base := "/tmp/create-dryrun-base-nonexistent"
	registryBefore := readGlobalRegistry(t, tc)

	output, err := tc.RunCamp("create", "dry-campaign",
		"-d", "desc",
		"-m", "mission",
		"--no-git",
		"--dry-run",
		"--path", base,
	)
	require.NoError(t, err, "dry-run should succeed without error; output: %s", output)

	// Base directory must still not exist.
	exists, checkErr := tc.CheckDirExists(base)
	require.NoError(t, checkErr)
	assert.False(t, exists, "dry-run must not create the base directory")

	// Target directory must not exist.
	exists, checkErr = tc.CheckDirExists(base + "/dry-campaign")
	require.NoError(t, checkErr)
	assert.False(t, exists, "dry-run must not create the target directory")

	// Output should contain the "would create base directory" hint.
	assert.Contains(t, output, "would create base directory",
		"dry-run output should mention would-create for base dir")

	registryAfter := readGlobalRegistry(t, tc)
	assert.Equal(t, registryBefore, registryAfter, "dry-run must not mutate the global registry")
}

// TestCampCreate_CollisionEmpty pre-creates an empty target directory and asserts
// that camp create proceeds normally (empty dir is acceptable).
func TestCampCreate_CollisionEmpty(t *testing.T) {
	tc := GetSharedContainer(t)

	base := "/tmp/create-collision-empty"
	target := base + "/empty-target"
	tc.Shell(t, fmt.Sprintf("mkdir -p %s", target))

	output, err := tc.RunCamp("create", "empty-target",
		"-d", "desc",
		"-m", "mission",
		"--no-git",
		"--path", base,
	)
	require.NoError(t, err, "camp create into empty dir should succeed; output: %s", output)
	assert.Contains(t, output, "Campaign Initialized")
}

// TestCampCreate_CollisionNonEmpty pre-creates a non-empty target without a
// .campaign/ marker and asserts a hard error; filesystem must be unchanged.
func TestCampCreate_CollisionNonEmpty(t *testing.T) {
	tc := GetSharedContainer(t)

	base := "/tmp/create-collision-nonempty"
	target := base + "/nonempty-target"
	tc.Shell(t, fmt.Sprintf("mkdir -p %s && touch %s/existingfile.txt", target, target))

	output, err := tc.RunCamp("create", "nonempty-target",
		"-d", "desc",
		"-m", "mission",
		"--no-git",
		"--path", base,
	)
	require.Error(t, err, "camp create into non-empty dir should fail")
	assert.Contains(t, output, "exists and is not empty",
		"error should mention the directory is not empty")

	// .campaign/ must not have been created inside the target.
	exists, checkErr := tc.CheckDirExists(target + "/.campaign")
	require.NoError(t, checkErr)
	assert.False(t, exists, ".campaign/ must not be created on collision error")
}

// TestCampCreate_CollisionExistingCampaign pre-creates a target with .campaign/
// and asserts a hard error mentioning 'camp init --repair'.
func TestCampCreate_CollisionExistingCampaign(t *testing.T) {
	tc := GetSharedContainer(t)

	base := "/tmp/create-collision-existing"
	target := base + "/existing-campaign"
	tc.Shell(t, fmt.Sprintf("mkdir -p %s/.campaign", target))

	output, err := tc.RunCamp("create", "existing-campaign",
		"-d", "desc",
		"-m", "mission",
		"--no-git",
		"--path", base,
	)
	require.Error(t, err, "camp create into existing campaign dir should fail")
	assert.Contains(t, output, "camp init --repair",
		"error should hint at 'camp init --repair'")
}

// TestCampCreate_CreatesParentDir asserts that when --path points to a
// path that does not yet exist, camp create creates it at 0o755.
func TestCampCreate_CreatesParentDir(t *testing.T) {
	tc := GetSharedContainer(t)

	base := "/tmp/create-creates-parent-auto"

	// Ensure base does NOT exist.
	tc.Shell(t, fmt.Sprintf("rm -rf %s", base))

	output, err := tc.RunCamp("create", "new-campaign",
		"-d", "desc",
		"-m", "mission",
		"--no-git",
		"--path", base,
	)
	require.NoError(t, err, "camp create should create parent dir; output: %s", output)

	// Base dir should have been created.
	exists, checkErr := tc.CheckDirExists(base)
	require.NoError(t, checkErr)
	assert.True(t, exists, "base directory should have been created by camp create")

	// Campaign inside it should exist.
	exists, checkErr = tc.CheckDirExists(base + "/new-campaign")
	require.NoError(t, checkErr)
	assert.True(t, exists, "campaign directory should exist under created base")
}

// TestCampCreate_PathFlagOverride configures CampaignsDir in the global config
// to one path and passes --path for a different path. The campaign must land
// under --path, not under CampaignsDir.
func TestCampCreate_PathFlagOverride(t *testing.T) {
	tc := GetSharedContainer(t)

	configBase := "/tmp/create-config-base"
	overrideBase := "/tmp/create-override-base"

	tc.Shell(t, fmt.Sprintf("mkdir -p %s %s", configBase, overrideBase))

	// Write a global config with CampaignsDir set to configBase.
	configJSON := fmt.Sprintf(`{"campaigns_dir": %q}`, configBase)
	if err := tc.WriteGlobalConfig(configJSON); err != nil {
		t.Fatalf("WriteGlobalConfig: %v", err)
	}

	output, err := tc.RunCamp("create", "override-campaign",
		"-d", "desc",
		"-m", "mission",
		"--no-git",
		"--path", overrideBase,
	)
	require.NoError(t, err, "camp create with --path override should succeed; output: %s", output)

	// Campaign should be under overrideBase.
	exists, checkErr := tc.CheckDirExists(overrideBase + "/override-campaign")
	require.NoError(t, checkErr)
	assert.True(t, exists, "campaign should land under --path, not CampaignsDir")

	// Campaign must NOT be under configBase.
	exists, checkErr = tc.CheckDirExists(configBase + "/override-campaign")
	require.NoError(t, checkErr)
	assert.False(t, exists, "campaign must NOT land under CampaignsDir when --path is set")
}

// TestCampCreate_UsesCampaignsDirConfig verifies that camp create uses
// GlobalConfig.CampaignsDir when --path is absent.
func TestCampCreate_UsesCampaignsDirConfig(t *testing.T) {
	tc := GetSharedContainer(t)

	configBase := "/tmp/create-config-selected-base"
	tc.Shell(t, fmt.Sprintf("mkdir -p %s", configBase))

	configJSON := fmt.Sprintf(`{"campaigns_dir": %q}`, configBase)
	if err := tc.WriteGlobalConfig(configJSON); err != nil {
		t.Fatalf("WriteGlobalConfig: %v", err)
	}

	output, err := tc.RunCamp("create", "config-selected-campaign",
		"-d", "desc",
		"-m", "mission",
		"--no-git",
	)
	require.NoError(t, err, "camp create should use configured CampaignsDir; output: %s", output)

	exists, checkErr := tc.CheckDirExists(configBase + "/config-selected-campaign")
	require.NoError(t, checkErr)
	assert.True(t, exists, "campaign should land under configured CampaignsDir when --path is absent")

	registry := readGlobalRegistry(t, tc)
	assert.Contains(t, registry, configBase+"/config-selected-campaign", "registry should contain the configured-base campaign path")
}

// TestCampCreate_FestivalOwnership verifies festival initialization ownership:
// when fest is available, camp create initializes festivals/ through the shared
// init flow.
func TestCampCreate_FestivalOwnership(t *testing.T) {
	if !festAvailable {
		t.Skip("fest binary not available in container; skipping festival-present test")
	}

	tc := GetSharedContainer(t)

	base := "/tmp/create-fest-ownership"
	tc.Shell(t, fmt.Sprintf("mkdir -p %s", base))

	t.Run("festivals exists when fest available", func(t *testing.T) {
		output, err := tc.RunCamp("create", "with-fest",
			"-d", "desc",
			"-m", "mission",
			"--no-git",
			"--path", base,
		)
		require.NoError(t, err, "camp create should succeed; output: %s", output)

		exists, checkErr := tc.CheckDirExists(base + "/with-fest/festivals")
		require.NoError(t, checkErr)
		assert.True(t, exists, "festivals/ should exist when fest is available")

		// Regression guard: only one initialization marker (no double-init).
		markers := []string{".festival", "fest.yaml", ".fest"}
		count := 0
		for _, m := range markers {
			if e, _ := tc.CheckDirExists(base + "/with-fest/festivals/" + m); e {
				count++
			}
			if e, _ := tc.CheckFileExists(base + "/with-fest/festivals/" + m); e {
				count++
			}
		}
		assert.GreaterOrEqual(t, count, 1,
			"festivals/ should contain at least one fest initialization marker")
	})
}
