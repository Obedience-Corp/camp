//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSkills_LinkWorktreesOnlyProjectsGitWorktree covers the containerized
// path for worktree skill projection (camp skills link --worktrees-only).
// Unit tests exercise the pure link helpers on host temp dirs; this test
// drives the real CLI against a fake worktree layout inside the harness.
func TestSkills_LinkWorktreesOnlyProjectsGitWorktree(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupSkillsCampaign(t, tc, "skills-worktree-proj")

	// Layout matches camp project worktree add: projects/worktrees/<proj>/<name>/
	// with a .git marker so ListWorktreeRoots accepts it.
	wt := path + "/projects/worktrees/demo/feature-x"
	tc.Shell(t, fmt.Sprintf(
		"mkdir -p %s && printf 'gitdir: /tmp/fake\\n' > %s/.git",
		wt, wt,
	))

	output, err := tc.RunCampInDir(path, "skills", "link", "--worktrees-only")
	require.NoError(t, err, "output: %s", output)
	assert.Contains(t, output, "feature-x")

	// Projected skill symlink under the worktree.
	link := wt + "/.agents/skills/" + testSkillSlug
	_, exitCode, err := tc.ExecCommand("test", "-L", link)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "expected projected skill link at %s; output: %s", link, output)

	// Grok alias points at .agents/skills.
	_, exitCode, err = tc.ExecCommand("test", "-L", wt+"/.grok/skills")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "expected .grok/skills alias")

	target, exitCode, err := tc.ExecCommand("readlink", wt+"/.grok/skills")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, target, ".agents/skills")

	// Status lists the worktree as linked.
	status, err := tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err)
	assert.Contains(t, status, "worktree")
	assert.Contains(t, status, "feature-x")
	assert.Contains(t, status, "linked")
}
