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

// A camp initialized while init pruned the quest scaffold gets it back lazily
// from `camp quest list`. Repair must keep that scaffold and every quest in it.
func TestInitRepair_KeepsQuestScaffoldCreatedByQuestList(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-keeps-lazy-quests")
	questRoot := path + "/" + quest.RootDirName

	_, exitCode, err := tc.ExecCommand("rm", "-rf", questRoot)
	require.NoError(t, err)
	require.Zero(t, exitCode)
	tc.GitOutput(t, path, "add", "-A")
	tc.GitOutput(t, path, "commit", "-m", "simulate a camp without the quest scaffold")

	out, err := tc.RunCampInDir(path, "quest", "list")
	require.NoError(t, err, out)
	assert.Contains(t, out, "default", "quest list should recreate the default quest")

	out, err = tc.RunCampInDir(path, "quest", "create", "kept",
		"--no-editor", "--purpose", "survive repair", "--no-commit")
	require.NoError(t, err, out)

	runRepair(t, tc, path)

	defaultQuest := fmt.Sprintf("%s/%s/%s", questRoot, quest.DefaultDirName, quest.FileName)
	exists, err := tc.CheckFileExists(defaultQuest)
	require.NoError(t, err)
	assert.True(t, exists, "repair should keep %s", defaultQuest)

	out, err = tc.RunCampInDir(path, "quest", "list")
	require.NoError(t, err, out)
	assert.Contains(t, out, "default", "repair should keep the default quest")
	assert.Contains(t, out, "kept", "repair should keep user quests")
}

// Completing the default quest moves it into the quest dungeon as qst_default.
// Repair must leave it addressable there instead of restoring a second default.
func TestInitRepair_KeepsCompletedDefaultQuestInDungeon(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-completed-default")
	defaultQuest := fmt.Sprintf("%s/%s/%s/%s", path, quest.RootDirName, quest.DefaultDirName, quest.FileName)

	out, err := tc.RunCampInDir(path, "quest", "complete", quest.DefaultQuestName)
	require.NoError(t, err, out)

	preview, err := tc.RunCampInDir(path, "init", "--repair", "--dry-run")
	require.NoError(t, err, preview)
	assert.NotContains(t, preview, quest.DefaultDirName+"/"+quest.FileName,
		"repair preview should not offer a second default quest")

	runRepair(t, tc, path)

	exists, err := tc.CheckFileExists(defaultQuest)
	require.NoError(t, err)
	assert.False(t, exists, "repair should not recreate %s", defaultQuest)

	out, err = tc.RunCampInDir(path, "quest", "show", quest.DefaultQuestID, "--json")
	require.NoError(t, err, out)
	assert.Regexp(t, `"status":\s*"completed"`, out, "qst_default should still resolve to the completed quest")
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
	assert.Contains(t, gi, "skills/.backup/",
		"camp init must seed .campaign/.gitignore with skills/.backup/: %s", gi)
}

func TestIntegration_InitRepair_AppendsSkillsBackupIfMissing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRepairCampaign(t, tc, "repair-gitignore-skills-backup")

	gi, err := tc.ReadFile(path + "/.campaign/.gitignore")
	require.NoError(t, err)
	stripped := strings.ReplaceAll(gi, "skills/.backup/\n", "")
	require.NotEqual(t, gi, stripped, "fixture must have the skills/.backup/ rule so we can remove it")
	require.NoError(t, tc.WriteFile(path+"/.campaign/.gitignore", stripped))
	tc.Shell(t, "cd "+path+" && git add .campaign/.gitignore && git commit -qm 'older camp gitignore' && sync")

	dry, err := tc.RunCampInDir(path, "init", "--repair", "--dry-run")
	require.NoError(t, err, dry)
	assert.Contains(t, dry, "missing skills/.backup/ entries")

	runRepair(t, tc, path)

	after, err := tc.ReadFile(path + "/.campaign/.gitignore")
	require.NoError(t, err)
	assert.Contains(t, after, "skills/.backup/", "repair must append skills/.backup/ when missing")
	assert.Empty(t, tc.GitOutput(t, path, "status", "--porcelain", "--", ".campaign/.gitignore"),
		"repair must commit the gitignore it appended to")
	checkOut := tc.Shell(t, "cd "+path+" && mkdir -p .campaign/skills/.backup/fest-execution-x && "+
		"touch .campaign/skills/.backup/fest-execution-x/SKILL.md && "+
		"git check-ignore -v .campaign/skills/.backup/fest-execution-x/SKILL.md")
	assert.Contains(t, checkOut, ".campaign/.gitignore:", "the camp gitignore must be the rule that ignores skills update backups")
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
