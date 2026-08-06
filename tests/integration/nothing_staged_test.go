//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The staged-only commit gate's no-op message.
//
// The gate looks at staged changes only, so it must not claim a clean working
// tree while unstaged or untracked dirt remains. That is the ordinary state at
// campaign root, where submodule pointers are excluded from staging by
// default, and telling a user their tree is clean there contradicts what
// `git status` shows them one command later.
//
// These messages are printed by three commands and were previously asserted by
// a host-side unit test that drove real git in a `t.TempDir`. They are user
// facing strings, so the binary is where they are worth pinning.

// checkNoOpLine runs a commit that stages nothing and returns its output.
func checkNoOpLine(t *testing.T, tc *TestContainer, dir string, args ...string) string {
	t.Helper()
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(dir, args...)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode,
		"a no-op commit must exit 0; stdout:\n%s\nstderr:\n%s", stdout, stderr)
	return stdout + stderr
}

func TestIntegration_NothingStagedMessageAtCampaignRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "nothing-staged-root")

	// A genuinely clean tree is the only case that may claim cleanliness.
	out := checkNoOpLine(t, tc, campPath, "commit", "--all=false", "-m", "no-op")
	assert.Contains(t, out, "Nothing to commit, working tree clean",
		"a clean tree must say so; output:\n%s", out)

	// Untracked dirt. "working tree clean" here would be a false statement
	// about a file the user can see in git status.
	tc.Shell(t, fmt.Sprintf("printf 'scratch\\n' > %s/untracked.md", campPath))
	out = checkNoOpLine(t, tc, campPath, "commit", "--all=false", "-m", "no-op")
	assert.Contains(t, out, "Nothing staged to commit",
		"untracked dirt must not report a clean tree; output:\n%s", out)
	assert.NotContains(t, out, "working tree clean",
		"untracked dirt must not report a clean tree; output:\n%s", out)

	// An unstaged change to a tracked file is the same claim from the other
	// direction: nothing is staged, but the tree is not clean either.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		rm -f untracked.md
		printf 'tracked\n' > tracked.md
		git add tracked.md
		git commit -q -m "add a tracked file"
		printf 'edited\n' > tracked.md
	`, campPath))
	out = checkNoOpLine(t, tc, campPath, "commit", "--all=false", "-m", "no-op")
	assert.Contains(t, out, "Nothing staged to commit",
		"an unstaged tracked change must not report a clean tree; output:\n%s", out)
	assert.NotContains(t, out, "working tree clean",
		"an unstaged tracked change must not report a clean tree; output:\n%s", out)
}

// The scoped form. A project commit names where it found nothing, because the
// user ran it from a directory that is not the campaign root and "nothing to
// commit" alone would leave them guessing which repository was meant.
func TestIntegration_NothingStagedMessageInProject(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "nothing-staged-project")

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "project", "new", "demo-app")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)
	projectPath := campPath + "/projects/demo-app"

	out := checkNoOpLine(t, tc, projectPath, "p", "commit", "--all=false", "-m", "no-op")
	assert.Contains(t, out, "Nothing to commit in project",
		"a clean project must name its scope; output:\n%s", out)

	tc.Shell(t, fmt.Sprintf("printf 'scratch\\n' > %s/untracked.md", projectPath))
	out = checkNoOpLine(t, tc, projectPath, "p", "commit", "--all=false", "-m", "no-op")
	assert.Contains(t, out, "Nothing staged to commit in project",
		"a dirty project must name its scope and not claim clean; output:\n%s", out)
	assert.NotContains(t, out, "Nothing to commit in project",
		"a dirty project must not report a clean tree; output:\n%s", out)
}

// The third caller. camp worktrees commit passes " in worktree", and a worktree
// is the scope most likely to be confused with the project it belongs to, so
// the qualifier is doing the most work here of the three.
func TestIntegration_NothingStagedMessageInWorktree(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "nothing-staged-worktree")

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "project", "new", "demo-app")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)
	projectPath := campPath + "/projects/demo-app"

	_, stderr, exitCode, err = tc.RunCampSplitInDir(projectPath, "p", "worktree", "add", "wt-scope")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "worktree add must succeed; stderr:\n%s", stderr)
	worktreePath := campPath + "/projects/worktrees/demo-app/wt-scope"

	out := checkNoOpLine(t, tc, worktreePath, "worktrees", "commit", "--all=false", "-m", "no-op")
	assert.Contains(t, out, "Nothing to commit in worktree",
		"a clean worktree must name its scope; output:\n%s", out)

	tc.Shell(t, fmt.Sprintf("printf 'scratch\\n' > %s/untracked.md", worktreePath))
	out = checkNoOpLine(t, tc, worktreePath, "worktrees", "commit", "--all=false", "-m", "no-op")
	assert.Contains(t, out, "Nothing staged to commit in worktree",
		"a dirty worktree must name its scope and not claim clean; output:\n%s", out)
	assert.NotContains(t, out, "Nothing to commit in worktree",
		"a dirty worktree must not report a clean tree; output:\n%s", out)
}
