//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// camp p commit run from inside a worktree used to fail ("project not found")
// because the generic project resolver only understands projects/<name> and
// submodule roots, not the projects/worktrees/<project>/<name> layout. It should
// now detect the worktree, commit there, and carry the workitem's WI-<ref> tag.
func TestIntegration_ProjectCommit_InWorktreeTagsWI(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/pcommit-worktree"
	initCommitTagsCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "wt-fix")

	_, err := tc.RunCampInDir(dir, "project", "new", "demo-app")
	require.NoError(t, err, "camp project new")

	out, err := tc.RunCampInDir(dir, "project", "worktree", "add", "wt-fix",
		"--project", "demo-app", "--workitem", "wt-fix")
	require.NoError(t, err, "worktree add: %s", out)

	wtRel := "projects/worktrees/demo-app/wt-fix"
	require.NoError(t, tc.WriteFile(dir+"/"+wtRel+"/foo.go", "package x\n"))

	commitOut, err := tc.RunCampInDir(dir+"/"+wtRel, "p", "commit", "-m", "feat: stub")
	require.NoError(t, err, "camp p commit inside worktree: %s", commitOut)

	subject := lastCommitSubject(t, tc, dir+"/"+wtRel)
	assert.Contains(t, subject, ref,
		"camp p commit from inside the worktree should carry the WI-<ref> tag: %s", subject)
}

// An explicit --project from inside a worktree must still target the named
// project's main checkout, not the worktree (the escape hatch is preserved).
func TestIntegration_ProjectCommit_ExplicitProjectFromWorktree(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/pcommit-worktree-explicit"
	initCommitTagsCampaign(t, tc, dir)
	seedDesignWorkitemWithRef(t, tc, dir, "wt-explicit")

	_, err := tc.RunCampInDir(dir, "project", "new", "demo-app")
	require.NoError(t, err, "camp project new")
	out, err := tc.RunCampInDir(dir, "project", "worktree", "add", "wt-explicit",
		"--project", "demo-app", "--workitem", "wt-explicit")
	require.NoError(t, err, "worktree add: %s", out)

	require.NoError(t, tc.WriteFile(dir+"/projects/demo-app/bar.go", "package y\n"))
	wtRel := "projects/worktrees/demo-app/wt-explicit"
	commitOut, err := tc.RunCampInDir(dir+"/"+wtRel, "p", "commit", "--project", "demo-app", "-m", "chore: bar")
	require.NoError(t, err, "camp p commit --project from worktree: %s", commitOut)

	subject := lastCommitSubject(t, tc, dir+"/projects/demo-app")
	assert.Contains(t, subject, "chore: bar",
		"explicit --project should commit in the project main checkout: %s", subject)
}
