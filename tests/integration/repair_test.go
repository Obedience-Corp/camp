//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRepairCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()

	path := "/campaigns/" + name
	_, err := tc.InitCampaign(path, name, "product")
	require.NoError(t, err)
	return path
}

func runRepair(t *testing.T, tc *TestContainer, campaignPath string, extraArgs ...string) string {
	t.Helper()

	args := append([]string{"init", "--repair", "--yes"}, extraArgs...)
	output, err := tc.RunCampInDir(campaignPath, args...)
	require.NoError(t, err, "repair failed: %s", output)
	return output
}

func TestInitRepair_UpToDateCampaignReportsNoChanges(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-up-to-date")

	output := runRepair(t, tc, path)
	assert.Contains(t, strings.ToLower(output), "up to date")
}

func TestInitRepair_RestoresMissingMiscFiles(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-missing-misc")

	_, _, err := tc.ExecCommand("sh", "-c", fmt.Sprintf("rm -f %s/.campaign/.gitignore %s/CLAUDE.md", path, path))
	require.NoError(t, err)

	runRepair(t, tc, path)

	exists, err := tc.CheckFileExists(path + "/.campaign/.gitignore")
	require.NoError(t, err)
	assert.True(t, exists, ".campaign/.gitignore should be restored")

	_, exitCode, err := tc.ExecCommand("test", "-L", path+"/CLAUDE.md")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "CLAUDE.md should be restored as a symlink")
}

func TestInitRepair_RestoresMissingStandardDungeonObey(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-missing-dungeon-obey")

	_, _, err := tc.ExecCommand("rm", "-f", path+"/workflow/design/dungeon/OBEY.md")
	require.NoError(t, err)

	runRepair(t, tc, path)

	exists, err := tc.CheckFileExists(path + "/workflow/design/dungeon/OBEY.md")
	require.NoError(t, err)
	assert.True(t, exists, "workflow/design/dungeon/OBEY.md should be restored")
}

func TestInitRepair_RestoresMissingQuestScaffoldWithoutActiveFile(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-quest-scaffold")

	removed := []string{
		fmt.Sprintf("%s/%s/%s/%s", path, quest.RootDirName, quest.DefaultDirName, quest.FileName),
		fmt.Sprintf("%s/%s/dungeon/OBEY.md", path, quest.RootDirName),
	}
	for _, item := range removed {
		_, _, err := tc.ExecCommand("rm", "-f", item)
		require.NoError(t, err)
	}

	runRepair(t, tc, path)

	for _, item := range removed {
		exists, err := tc.CheckFileExists(item)
		require.NoError(t, err)
		assert.True(t, exists, "repair should restore %s", item)
	}

	activeExists, err := tc.CheckFileExists(fmt.Sprintf("%s/%s/.active", path, quest.RootDirName))
	require.NoError(t, err)
	assert.False(t, activeExists, "repair should not create quests/.active")
}

func TestInitRepair_RestoresMissingSkillFiles(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-skill-files")

	removed := []string{
		path + "/.campaign/skills/camp-navigation/SKILL.md",
		path + "/.campaign/skills/references/camp-command-contracts.md",
	}
	for _, item := range removed {
		_, _, err := tc.ExecCommand("rm", "-f", item)
		require.NoError(t, err)
	}

	runRepair(t, tc, path)

	for _, item := range removed {
		exists, err := tc.CheckFileExists(item)
		require.NoError(t, err)
		assert.True(t, exists, "repair should restore %s", item)
	}
}

func TestInitRepair_PreservesUserShortcuts(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-user-shortcuts")

	// Create the target directory for the shortcut.
	_, _, err := tc.ExecCommand("mkdir", "-p", path+"/my-stuff")
	require.NoError(t, err)

	// Add a user shortcut via the real CLI path — exercises config.SaveJumpsConfig
	// under the hood, so the fixture YAML is guaranteed to match the config schema.
	addOutput, err := tc.RunCampInDir(path, "shortcuts", "add", "custom", "my-stuff/", "-d", "User custom shortcut")
	require.NoError(t, err, "shortcuts add failed: %s", addOutput)

	// Verify the shortcut was created before repair.
	preRepairOutput, err := tc.RunCampInDir(path, "go", "custom", "--print")
	require.NoError(t, err, "pre-repair navigation failed: %s", preRepairOutput)
	assert.Contains(t, preRepairOutput, path+"/my-stuff")

	// Run repair — should preserve the user-defined shortcut.
	runRepair(t, tc, path)

	// Verify the shortcut survives repair in the config file.
	jumpsPath := path + "/.campaign/settings/jumps.yaml"
	updatedJumps, err := tc.ReadFile(jumpsPath)
	require.NoError(t, err)
	assert.Contains(t, updatedJumps, "custom:", "user shortcut key should survive repair")
	assert.Contains(t, updatedJumps, "my-stuff/", "user shortcut path should survive repair")

	// Verify navigation still works after repair.
	output, err := tc.RunCampInDir(path, "go", "custom", "--print")
	require.NoError(t, err)
	assert.Contains(t, output, path+"/my-stuff")
}
