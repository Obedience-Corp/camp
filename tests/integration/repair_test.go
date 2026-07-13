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
		path + "/.campaign/skills/campaign-commit/SKILL.md",
		path + "/.campaign/skills/camp-projects/SKILL.md",
		path + "/.campaign/skills/camp-workitems/SKILL.md",
		path + "/.campaign/skills/fest-execution/SKILL.md",
		path + "/.campaign/skills/fest-standalone-workflows/SKILL.md",
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

func TestIntegration_Init_GitignoreLocalState(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "init-gitignore-current")

	gi, err := tc.ReadFile(path + "/.campaign/.gitignore")
	require.NoError(t, err)
	assert.Contains(t, gi, "workitems/current.yaml",
		"camp init must seed .campaign/.gitignore with workitems/current.yaml: %s", gi)
	assert.Contains(t, gi, "events/",
		"camp init must seed .campaign/.gitignore with events/: %s", gi)
}

func TestIntegration_InitRepair_AppendsCurrentYamlIfMissing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-gitignore-current")

	gi, err := tc.ReadFile(path + "/.campaign/.gitignore")
	require.NoError(t, err)
	stripped := strings.ReplaceAll(gi, "workitems/current.yaml\n", "")
	require.NotEqual(t, gi, stripped, "fixture must have the line so we can remove it")
	require.NoError(t, tc.WriteFile(path+"/.campaign/.gitignore", stripped))

	runRepair(t, tc, path)

	after, err := tc.ReadFile(path + "/.campaign/.gitignore")
	require.NoError(t, err)
	assert.Contains(t, after, "workitems/current.yaml",
		"repair must append workitems/current.yaml when missing")
}

func TestIntegration_InitRepair_AppendsEventsIfMissing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-gitignore-events")

	gi, err := tc.ReadFile(path + "/.campaign/.gitignore")
	require.NoError(t, err)
	stripped := strings.ReplaceAll(gi, "events/\n", "")
	require.NotEqual(t, gi, stripped, "fixture must have the events rule so we can remove it")
	require.NoError(t, tc.WriteFile(path+"/.campaign/.gitignore", stripped))

	runRepair(t, tc, path)

	after, err := tc.ReadFile(path + "/.campaign/.gitignore")
	require.NoError(t, err)
	assert.Contains(t, after, "events/", "repair must append events/ when missing")
}

func TestIntegration_InitRepair_AppendsCurrentYamlWhenCommentedOut(t *testing.T) {
	// Regression for PR #311 review: presence check must use gitignore-line
	// semantics, not raw substring. A commented-out line like
	// `# workitems/current.yaml` does NOT make git ignore the file, so
	// repair must still append the active rule.
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-gitignore-commented")

	gi, err := tc.ReadFile(path + "/.campaign/.gitignore")
	require.NoError(t, err)
	// Replace the active rule with a commented-out version. A substring-only
	// check would see "workitems/current.yaml" in the file and skip the
	// repair; the line-rule check sees it is commented out and appends.
	commented := strings.Replace(gi,
		"workitems/current.yaml",
		"# workitems/current.yaml",
		1)
	require.NotEqual(t, gi, commented, "fixture must have the line so we can comment it")
	require.NoError(t, tc.WriteFile(path+"/.campaign/.gitignore", commented))

	runRepair(t, tc, path)

	after, err := tc.ReadFile(path + "/.campaign/.gitignore")
	require.NoError(t, err)
	checkOut, _, err := tc.ExecCommand("sh", "-c",
		"mkdir -p "+path+"/.campaign/workitems && touch "+path+"/.campaign/workitems/current.yaml && "+
			"git -C "+path+" init -q 2>/dev/null; git -C "+path+" check-ignore -v "+
			".campaign/workitems/current.yaml")
	require.NoError(t, err)
	assert.Contains(t, checkOut, "workitems/current.yaml",
		"after repair, git must actually ignore current.yaml; .gitignore is:\n%s\ncheck-ignore: %s",
		after, checkOut)
}

func TestIntegration_WorkitemCurrent_ProducesIgnoredFile(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "current-ignored")
	require.NoError(t, tc.CreateGitRepo(path))

	out, err := tc.RunCampInDir(path, "workitem", "create", "ignored-test", "--type", "design", "--title", "x")
	require.NoError(t, err, "create: %s", out)

	_, err = tc.RunCampInDir(path, "workitem", "current", "ignored-test")
	require.NoError(t, err)

	checkOut, _, err := tc.ExecCommand("git", "-C", path, "check-ignore", "-v",
		".campaign/workitems/current.yaml")
	require.NoError(t, err)
	assert.Contains(t, checkOut, "workitems/current.yaml",
		"current.yaml must match a .gitignore rule, got:\n%s", checkOut)
}
