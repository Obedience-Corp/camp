//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInit_AutoLinksSkills verifies that camp init projects scaffolded skill
// bundles into both .claude/skills and .agents/skills with no extra command.
func TestInit_AutoLinksSkills(t *testing.T) {
	tc := GetSharedContainer(t)

	path := "/campaigns/test-init-autolink"
	output, err := tc.InitCampaign(path, "test-init-autolink", "product")
	require.NoError(t, err)
	assert.Contains(t, output, "Skills:", "init summary should report skills linking")

	for _, rel := range []string{".claude/skills", ".agents/skills"} {
		link := path + "/" + rel + "/camp-workitems"
		_, exitCode, err := tc.ExecCommand("test", "-L", link)
		require.NoError(t, err)
		assert.Equal(t, 0, exitCode, "expected projected symlink at %s", rel)

		target, exitCode, err := tc.ExecCommand("readlink", link)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode)
		assert.Contains(t, strings.TrimSpace(target), ".campaign/skills/camp-workitems")
	}
}

// TestInit_NoSkillsFlag verifies --no-skills scaffolds .campaign/skills but
// projects nothing into tool directories.
func TestInit_NoSkillsFlag(t *testing.T) {
	tc := GetSharedContainer(t)

	path := "/campaigns/test-init-no-skills"
	_, err := tc.RunCamp("init", path, "--name", "test-init-no-skills",
		"-d", "Test campaign", "-m", "Test mission", "--type", "product", "--no-skills")
	require.NoError(t, err)

	exists, err := tc.CheckFileExists(path + "/.campaign/skills/camp-workitems/SKILL.md")
	require.NoError(t, err)
	assert.True(t, exists, ".campaign/skills should still be scaffolded")

	_, exitCode, err := tc.ExecCommand("test", "-e", path+"/.claude/skills")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, ".claude/skills should not exist with --no-skills")
}

// TestInit_RepairHealsBrokenSkillLink verifies that init --repair re-projects a
// deleted skill link and is idempotent on a second run.
func TestInit_RepairHealsBrokenSkillLink(t *testing.T) {
	tc := GetSharedContainer(t)

	path := "/campaigns/test-init-repair-skills"
	_, err := tc.InitCampaign(path, "test-init-repair-skills", "product")
	require.NoError(t, err)

	link := path + "/.claude/skills/camp-workitems"
	_, exitCode, err := tc.ExecCommand("rm", link)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	_, err = tc.RunCampInDir(path, "init", "--repair", "--yes")
	require.NoError(t, err)

	_, exitCode, err = tc.ExecCommand("test", "-L", link)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "repair should restore the deleted skill link")

	// Second repair is a no-op for skills (idempotent).
	_, err = tc.RunCampInDir(path, "init", "--repair", "--yes")
	require.NoError(t, err)
	_, exitCode, err = tc.ExecCommand("test", "-L", link)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

// TestInit_RepairCommitsProjectedSkillLinks reproduces an older campaign with no
// projected tool skills and verifies that 'camp init --repair --yes' both
// recreates the links AND commits them (the selective repair commit must stage
// the newly projected paths, not leave them untracked).
func TestInit_RepairCommitsProjectedSkillLinks(t *testing.T) {
	tc := GetSharedContainer(t)

	path := "/campaigns/test-init-repair-commit"
	_, err := tc.InitCampaign(path, "test-init-repair-commit", "product")
	require.NoError(t, err)

	// Simulate an older campaign: remove all projected tool skills and commit
	// that removal so the working tree starts clean with no .claude/.agents links.
	_, exitCode, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && rm -rf .claude/skills .agents/skills && git add -A && git commit -q -m 'remove projected skills' && sync")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	_, err = tc.RunCampInDir(path, "init", "--repair", "--yes")
	require.NoError(t, err)

	for _, rel := range []string{".claude/skills/camp-workitems", ".agents/skills/camp-workitems"} {
		_, exitCode, err = tc.ExecCommand("test", "-L", path+"/"+rel)
		require.NoError(t, err)
		assert.Equal(t, 0, exitCode, "repair should restore %s", rel)

		// The link must be tracked in git (committed), not left untracked.
		out, exitCode, err := tc.ExecCommand("sh", "-c",
			"cd "+path+" && git ls-files --error-unmatch "+rel+" 2>&1")
		require.NoError(t, err)
		assert.Equal(t, 0, exitCode, "repair should commit %s (git output: %s)", rel, strings.TrimSpace(out))
	}

	// Working tree should be clean: repair committed everything it created.
	out, exitCode, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.Empty(t, strings.TrimSpace(out), "repair should leave a clean working tree")
}

// TestSkills_LinkNoFlagsProjectsAllTools verifies that bare 'camp skills link'
// projects into every registered tool.
func TestSkills_LinkNoFlagsProjectsAllTools(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-link-all")

	output, err := tc.RunCampInDir(path, "skills", "link")
	require.NoError(t, err)
	assert.Contains(t, output, ".claude/skills")
	assert.Contains(t, output, ".agents/skills")

	for _, rel := range []string{".claude/skills", ".agents/skills"} {
		_, exitCode, err := tc.ExecCommand("test", "-L", path+"/"+rel+"/"+testSkillSlug)
		require.NoError(t, err)
		assert.Equal(t, 0, exitCode, "expected projected symlink at %s", rel)
	}
}
