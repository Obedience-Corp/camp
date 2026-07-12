//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_WorkitemWorktree_InfersProjectAndLinks(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-worktree"
	initCommitTagsCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "settings-tui")

	_, err := tc.RunCampInDir(dir, "project", "new", "camp-app")
	require.NoError(t, err, "camp project new")
	_, err = tc.RunCampInDir(dir, "workitem", "link", "settings-tui", "--project", "camp-app")
	require.NoError(t, err, "workitem link --project")

	out, err := tc.RunCampInDir(dir, "workitem", "worktree", "settings-tui")
	require.NoError(t, err, "camp workitem worktree: %s", out)

	wtRel := "projects/worktrees/camp-app/settings-tui"
	exists, err := tc.CheckDirExists(dir + "/" + wtRel)
	require.NoError(t, err)
	require.True(t, exists, "worktree dir should exist at %s; output:\n%s", wtRel, out)

	resolveOut, err := tc.RunCampInDir(dir+"/"+wtRel, "workitem", "resolve")
	require.NoError(t, err, "resolve from worktree: %s", resolveOut)
	assert.Contains(t, resolveOut, "settings-tui",
		"resolving from inside the worktree should surface the linked workitem: %s", resolveOut)

	require.NoError(t, tc.WriteFile(dir+"/"+wtRel+"/foo.go", "package x\n"))
	commitOut, err := tc.RunCampInDir(dir+"/"+wtRel, "worktrees", "commit", "-m", "feat: stub")
	require.NoError(t, err, "camp worktrees commit in worktree: %s", commitOut)
	subject := lastCommitSubject(t, tc, dir+"/"+wtRel)
	assert.Contains(t, subject, ref,
		"commit made inside the linked worktree should carry the WI-<ref> tag: %s", subject)

	printOut, err := tc.RunCampInDir(dir, "workitem", "worktree", "settings-tui", "--print")
	require.NoError(t, err, "re-entry --print: %s", printOut)
	assert.Equal(t, wtRel, strings.TrimSpace(printOut),
		"re-entry must return the existing worktree path without creating a new one")
}

func TestIntegration_WorkitemWorktree_RequiresProjectWhenUnlinked(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-worktree-unlinked"
	initCommitTagsCampaign(t, tc, dir)
	seedDesignWorkitemWithRef(t, tc, dir, "orphan")

	out, err := tc.RunCampInDir(dir, "workitem", "worktree", "orphan")
	require.Error(t, err, "must require --project when the workitem has no linked project; output:\n%s", out)
	assert.Contains(t, out, "no linked project",
		"error should name the missing-project cause: %s", out)
}
