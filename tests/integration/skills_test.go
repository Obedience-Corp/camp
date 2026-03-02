//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSkillSlug = "code-review"

// setupSkillsCampaign creates a campaign with a .campaign/skills/ directory.
func setupSkillsCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()

	path := "/campaigns/" + name
	_, err := tc.InitCampaign(path, name, "product")
	require.NoError(t, err)

	// Ensure .campaign/skills/<slug>/SKILL.md exists
	_, exitCode, err := tc.ExecCommand("mkdir", "-p", path+"/.campaign/skills/"+testSkillSlug)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	err = tc.WriteFile(path+"/.campaign/skills/"+testSkillSlug+"/SKILL.md", `---
name: Code Review
description: Review code for safety and correctness.
---`)
	require.NoError(t, err)

	return path
}

// TestSkills_LinkLifecycle tests the full link -> status -> unlink flow.
func TestSkills_LinkLifecycle(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-lifecycle")

	// Link claude
	output, err := tc.RunCampInDir(path, "skills", "link", "--tool", "claude")
	require.NoError(t, err)
	assert.Contains(t, output, "projected")
	assert.Contains(t, output, ".claude/skills")

	// Verify projected skill symlink exists and points to the right place
	linkTarget, exitCode, err := tc.ExecCommand("readlink", path+"/.claude/skills/"+testSkillSlug)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, strings.TrimSpace(linkTarget), ".campaign/skills/"+testSkillSlug)

	// Status should show linked
	output, err = tc.RunCampInDir(path, "skills", "status")
	// status may return error if agents is not linked, but output should show claude as linked
	assert.Contains(t, output, "linked")
	assert.Contains(t, output, "claude")

	// Unlink claude
	output, err = tc.RunCampInDir(path, "skills", "unlink", "--tool", "claude")
	require.NoError(t, err)
	assert.Contains(t, output, "unlinked")

	// Verify projected symlink is gone
	_, exitCode, err = tc.ExecCommand("test", "-e", path+"/.claude/skills/"+testSkillSlug)
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "projected skill symlink should be removed")
}

// TestSkills_LinkAlreadyLinked verifies idempotent linking.
func TestSkills_LinkAlreadyLinked(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-already-linked")

	// Link claude
	_, err := tc.RunCampInDir(path, "skills", "link", "--tool", "claude")
	require.NoError(t, err)

	// Link again — should report already linked
	output, err := tc.RunCampInDir(path, "skills", "link", "--tool", "claude")
	require.NoError(t, err)
	assert.Contains(t, output, "already linked")
}

// TestSkills_LinkForce tests force replacing a conflicting foreign symlink entry.
func TestSkills_LinkForce(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-force")

	// Create a foreign symlink at the projected skill entry.
	_, exitCode, err := tc.ExecCommand("mkdir", "-p", path+"/.claude/skills")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	_, exitCode, err = tc.ExecCommand("ln", "-s", "/tmp", path+"/.claude/skills/"+testSkillSlug)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Link without --force should fail due conflict
	_, err = tc.RunCampInDir(path, "skills", "link", "--tool", "claude")
	assert.Error(t, err, "should fail without --force when conflicting symlink exists")

	// Link with --force should replace conflicting symlink entry
	output, err := tc.RunCampInDir(path, "skills", "link", "--tool", "claude", "--force")
	require.NoError(t, err)
	assert.Contains(t, output, "projected")

	// Verify projected skill link now points to campaign skills
	linkTarget, exitCode, err := tc.ExecCommand("readlink", path+"/.claude/skills/"+testSkillSlug)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, strings.TrimSpace(linkTarget), ".campaign/skills/"+testSkillSlug)
}

// TestSkills_StatusMixed tests status with mixed link states.
func TestSkills_StatusMixed(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-status-mixed")

	// Link claude (valid)
	_, err := tc.RunCampInDir(path, "skills", "link", "--tool", "claude")
	require.NoError(t, err)

	// agents is not linked — should show "not linked"

	// Status output should show claude as linked and agents as not linked
	output, _ := tc.RunCampInDir(path, "skills", "status")
	assert.Contains(t, output, "linked")
	assert.Contains(t, output, "not linked")
}

// TestSkills_StatusJSON tests JSON output format.
func TestSkills_StatusJSON(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-status-json")

	// Link claude
	_, err := tc.RunCampInDir(path, "skills", "link", "--tool", "claude")
	require.NoError(t, err)

	// Status with --json
	output, _ := tc.RunCampInDir(path, "skills", "status", "--json")
	assert.Contains(t, output, `"state"`)
	assert.Contains(t, output, `"tool"`)
	assert.Contains(t, output, `"claude"`)
}

// TestSkills_UnlinkNonManaged tests that unlink refuses to remove non-managed symlinks.
func TestSkills_UnlinkNonManaged(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-unlink-safety")

	// Create a symlink pointing somewhere else (not .campaign/skills)
	_, exitCode, err := tc.ExecCommand("mkdir", "-p", path+"/.claude")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	_, exitCode, err = tc.ExecCommand("ln", "-s", "/tmp", path+"/.claude/skills")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Unlink should refuse
	_, err = tc.RunCampInDir(path, "skills", "unlink", "--tool", "claude")
	assert.Error(t, err, "should refuse to remove non-managed symlink")
}

// TestSkills_UnlinkMissing tests that unlink handles missing paths gracefully.
func TestSkills_UnlinkMissing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-unlink-missing")

	// Unlink something that doesn't exist — should report "not linked"
	output, err := tc.RunCampInDir(path, "skills", "unlink", "--tool", "claude")
	require.NoError(t, err)
	assert.Contains(t, output, "not linked")
}

// TestSkills_DryRun tests dry-run mode for link and unlink.
func TestSkills_DryRun(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-dryrun")

	// Dry run link — should show what would happen
	output, err := tc.RunCampInDir(path, "skills", "link", "--tool", "claude", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, output, "would project")

	// Verify no symlink was actually created
	_, exitCode, err := tc.ExecCommand("test", "-e", path+"/.claude/skills/"+testSkillSlug)
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "dry run should not create projected symlink")

	// Actually link it
	_, err = tc.RunCampInDir(path, "skills", "link", "--tool", "claude")
	require.NoError(t, err)

	// Dry run unlink — should show what would happen
	output, err = tc.RunCampInDir(path, "skills", "unlink", "--tool", "claude", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, output, "would remove")

	// Verify projected symlink still exists after dry run
	_, exitCode, err = tc.ExecCommand("test", "-L", path+"/.claude/skills/"+testSkillSlug)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "dry run should not remove projected symlink")
}

// TestSkills_LinkAgents tests linking the agents tool.
func TestSkills_LinkAgents(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-agents")

	// Link agents
	output, err := tc.RunCampInDir(path, "skills", "link", "--tool", "agents")
	require.NoError(t, err)
	assert.Contains(t, output, "projected")
	assert.Contains(t, output, ".agents/skills")

	// Verify projected skill symlink
	linkTarget, exitCode, err := tc.ExecCommand("readlink", path+"/.agents/skills/"+testSkillSlug)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, strings.TrimSpace(linkTarget), ".campaign/skills/"+testSkillSlug)
}

// TestSkills_LinkCustomPath tests linking with --path flag.
func TestSkills_LinkCustomPath(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-custom-path")

	// Link with custom path
	output, err := tc.RunCampInDir(path, "skills", "link", "--path", "custom/tools/skills")
	require.NoError(t, err)
	assert.Contains(t, output, "projected")

	// Verify projected skill symlink exists
	linkTarget, exitCode, err := tc.ExecCommand("readlink", path+"/custom/tools/skills/"+testSkillSlug)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, strings.TrimSpace(linkTarget), ".campaign/skills/"+testSkillSlug)
}

// TestSkills_LinkRejectsSymlinkEscape ensures custom paths cannot escape the
// campaign root through symlinked parent directories.
func TestSkills_LinkRejectsSymlinkEscape(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-symlink-escape")

	// Create an out-of-root directory and a symlink inside campaign pointing to it.
	outsideDir, exitCode, err := tc.ExecCommand("mktemp", "-d")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	outsideDir = strings.TrimSpace(outsideDir)

	_, exitCode, err = tc.ExecCommand("ln", "-s", outsideDir, path+"/escape")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Without --force, link should reject escaped destination.
	_, err = tc.RunCampInDir(path, "skills", "link", "--path", "escape/customskills")
	assert.Error(t, err, "escaped symlink parent path should be rejected")

	// Ensure no out-of-root symlink was created.
	_, exitCode, err = tc.ExecCommand("test", "-L", outsideDir+"/customskills/"+testSkillSlug)
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "escaped destination should not be created")

	// With --force and an existing file, command should still reject and not mutate it.
	err = tc.WriteFile(outsideDir+"/customskills", "keep me")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(path, "skills", "link", "--path", "escape/customskills", "--force")
	assert.Error(t, err, "escaped destination should be rejected even with --force")

	_, exitCode, err = tc.ExecCommand("test", "-f", outsideDir+"/customskills")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "existing out-of-root file must remain")

	_, exitCode, err = tc.ExecCommand("test", "-L", outsideDir+"/customskills")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "existing out-of-root file must not be replaced with symlink")
}
