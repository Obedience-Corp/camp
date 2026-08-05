//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// The staged-only commit gate must never claim a clean worktree while unstaged
// or untracked dirt remains. Each commit surface carries its own place
// qualifier, so the whole message matrix is asserted through the real commands
// rather than by calling the helper against a host temp dir.

func TestIntegration_NothingStagedMessagesAtCampaignRoot(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/nothing-staged-root"
	_, err := tc.InitCampaign(campaignDir, "Nothing Staged Root", "product")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(campaignDir, "commit", "--all=false", "-m", "nothing here")
	require.NoError(t, err, "a clean tree should be a friendly no-op; output:\n%s", out)
	require.Contains(t, out, "Nothing to commit, working tree clean")

	// Untracked dirt with an empty index is the case that must not be
	// described as clean: the user still sees the file in git status.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'dirt\n' > untracked.txt
	`, campaignDir))

	out, err = tc.RunCampInDir(campaignDir, "commit", "--all=false", "-m", "still nothing")
	require.NoError(t, err, "output:\n%s", out)
	require.Contains(t, out, "Nothing staged to commit")
	require.NotContains(t, out, "working tree clean",
		"untracked dirt remains, so the tree must not be reported clean")
}

func TestIntegration_NothingStagedMessagesInProject(t *testing.T) {
	tc := GetSharedContainer(t)

	campaignDir, projectRel := setupSubmoduleCampaign(t, tc, "nothing-staged-project")
	projectDir := campaignDir + "/" + projectRel

	out, err := tc.RunCampInDir(projectDir, "p", "commit", "--all=false", "-m", "nothing here")
	require.NoError(t, err, "a clean project should be a friendly no-op; output:\n%s", out)
	require.Contains(t, out, "Nothing to commit in project")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'dirt\n' > untracked.txt
	`, projectDir))

	out, err = tc.RunCampInDir(projectDir, "p", "commit", "--all=false", "-m", "still nothing")
	require.NoError(t, err, "output:\n%s", out)
	require.Contains(t, out, "Nothing staged to commit in project")
}

func TestIntegration_NothingStagedMessagesInWorktree(t *testing.T) {
	tc := GetSharedContainer(t)

	campaignDir, projectRel := setupSubmoduleCampaign(t, tc, "nothing-staged-worktree")
	projectDir := campaignDir + "/" + projectRel

	out, err := tc.RunCampInDir(projectDir, "p", "worktree", "add", "wt-scope")
	require.NoError(t, err, "worktree add should succeed; output:\n%s", out)

	worktreeDir := campaignDir + "/projects/worktrees/widget/wt-scope"
	tc.Shell(t, fmt.Sprintf("test -d %s && echo WT_PRESENT", worktreeDir))

	out, err = tc.RunCampInDir(worktreeDir, "worktrees", "commit", "--all=false", "-m", "nothing here")
	require.NoError(t, err, "a clean worktree should be a friendly no-op; output:\n%s", out)
	require.Contains(t, out, "Nothing to commit in worktree")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'dirt\n' > untracked.txt
	`, worktreeDir))

	out, err = tc.RunCampInDir(worktreeDir, "worktrees", "commit", "--all=false", "-m", "still nothing")
	require.NoError(t, err, "output:\n%s", out)
	require.Contains(t, out, "Nothing staged to commit in worktree")
}
