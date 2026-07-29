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

// realWorktree creates a real git worktree checkout under the campaign's
// projects/worktrees layout so ListWorktreeRoots + info/exclude work.
func realWorktree(t *testing.T, tc *TestContainer, campaignPath, project, name string) string {
	t.Helper()
	// Source repo for the worktree (a minimal git repo).
	src := campaignPath + "/projects/" + project
	tc.Shell(t, fmt.Sprintf(`
		mkdir -p %[1]s && cd %[1]s &&
		git init -q &&
		git config user.email test@example.com &&
		git config user.name test &&
		echo ok > README && git add README && git commit -q -m init
	`, src))

	wt := campaignPath + "/projects/worktrees/" + project + "/" + name
	tc.Shell(t, fmt.Sprintf(
		"mkdir -p %s && git -C %s worktree add -q %s -b %s",
		campaignPath+"/projects/worktrees/"+project, src, wt, name,
	))
	return wt
}

// TestSkills_LinkWorktreesOnlyProjectsGitWorktree covers the containerized
// path for worktree skill projection (camp skills link --worktrees-only).
func TestSkills_LinkWorktreesOnlyProjectsGitWorktree(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-worktree-proj")
	wt := realWorktree(t, tc, path, "demo", "feature-x")

	output, err := tc.RunCampInDir(path, "skills", "link", "--worktrees-only")
	require.NoError(t, err, "output: %s", output)
	assert.Contains(t, output, "feature-x")

	// Projected skill symlink under the worktree.
	link := wt + "/.agents/skills/" + testSkillSlug
	_, exitCode, err := tc.ExecCommand("test", "-L", link)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "expected projected skill link at %s; output: %s", link, output)

	// Claude surface too.
	_, exitCode, err = tc.ExecCommand("test", "-L", wt+"/.claude/skills/"+testSkillSlug)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "expected .claude projected skill link")

	// Grok alias points at .agents/skills.
	_, exitCode, err = tc.ExecCommand("test", "-L", wt+"/.grok/skills")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "expected .grok/skills alias")

	target, exitCode, err := tc.ExecCommand("readlink", wt+"/.grok/skills")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, target, ".agents/skills")

	// Local excludes so projections are not untracked in the target repo.
	exclude, exitCode, err := tc.ExecCommand("sh", "-c",
		"git -C "+wt+" rev-parse --git-path info/exclude | xargs cat")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "read info/exclude: %s", exclude)
	for _, pattern := range []string{".agents/", ".claude/", ".grok/"} {
		assert.Contains(t, exclude, pattern, "info/exclude missing %s", pattern)
	}
	// Untracked list should not show harness dirs.
	untracked, exitCode, err := tc.ExecCommand("git", "-C", wt, "status", "--porcelain")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.NotContains(t, untracked, ".agents/")
	assert.NotContains(t, untracked, ".claude/")
	assert.NotContains(t, untracked, ".grok/")

	// Status lists the worktree as linked.
	status, err := tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err)
	assert.Contains(t, status, "worktree")
	assert.Contains(t, status, "feature-x")
	assert.Contains(t, status, "linked")
}

// TestSkills_WorktreeGrokForeignSymlinkRequiresForce ensures a user-owned
// .grok/skills symlink is not silently replaced without --force.
func TestSkills_WorktreeGrokForeignSymlinkRequiresForce(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-worktree-force")
	wt := realWorktree(t, tc, path, "demo2", "feature-y")

	// Pre-create foreign grok alias.
	tc.Shell(t, fmt.Sprintf(
		"mkdir -p %s/.grok && ln -s /tmp/user-skills %s/.grok/skills",
		wt, wt,
	))

	output, err := tc.RunCampInDir(path, "skills", "link", "--worktrees-only")
	// Conflict should surface as a non-zero exit or explicit incomplete message.
	if err == nil {
		// Some paths report conflicts as exit 0 with message — require conflict wording.
		require.True(t,
			strings.Contains(output, "conflict") || strings.Contains(output, "incomplete"),
			"expected conflict report without --force; output: %s", output)
	}

	// Foreign target preserved.
	target, exitCode, err := tc.ExecCommand("readlink", wt+"/.grok/skills")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, target, "/tmp/user-skills")

	// Force replaces.
	output, err = tc.RunCampInDir(path, "skills", "link", "--worktrees-only", "--force")
	require.NoError(t, err, "force link: %s", output)
	target, exitCode, err = tc.ExecCommand("readlink", wt+"/.grok/skills")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, target, ".agents/skills")
}

// TestSkills_WorktreeGrokDirectoryGetsProjectedBundles covers a real
// .grok/skills directory (not a symlink): campaign bundles must be linked into it.
func TestSkills_WorktreeGrokDirectoryGetsProjectedBundles(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-worktree-grokdir")
	wt := realWorktree(t, tc, path, "demo3", "feature-z")

	tc.Shell(t, fmt.Sprintf("mkdir -p %s/.grok/skills", wt))

	output, err := tc.RunCampInDir(path, "skills", "link", "--worktrees-only")
	require.NoError(t, err, "output: %s", output)

	// Directory remains a directory.
	_, exitCode, err := tc.ExecCommand("test", "-d", wt+"/.grok/skills")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	_, exitCode, err = tc.ExecCommand("test", "!", "-L", wt+"/.grok/skills")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)

	// Managed bundle inside the directory.
	_, exitCode, err = tc.ExecCommand("test", "-L", wt+"/.grok/skills/"+testSkillSlug)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "expected managed skill link inside .grok/skills dir")
}

// TestSkills_WorktreeStatusAggregatesSurfaces: broken claude while agents ok
// must not report fully linked.
func TestSkills_WorktreeStatusAggregatesSurfaces(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-worktree-status")
	wt := realWorktree(t, tc, path, "demo4", "feature-s")

	_, err := tc.RunCampInDir(path, "skills", "link", "--worktrees-only")
	require.NoError(t, err)

	// Break claude surface: replace skills dir with a file.
	tc.Shell(t, fmt.Sprintf("rm -rf %s/.claude/skills && mkdir -p %s/.claude && echo x > %s/.claude/skills", wt, wt, wt))

	status, err := tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err)
	assert.Contains(t, status, "feature-s")
	// Must not claim fully linked when a surface is blocked.
	assert.NotContains(t, status, fmt.Sprintf("feature-s%slinked", strings.Repeat(" ", 10)))
	// Expect blocked/partial/conflict wording somewhere for the worktree row.
	assert.True(t,
		strings.Contains(status, "blocked") ||
			strings.Contains(status, "partial") ||
			strings.Contains(status, "conflict") ||
			strings.Contains(status, "not linked"),
		"status should not look fully healthy; got:\n%s", status)
}
