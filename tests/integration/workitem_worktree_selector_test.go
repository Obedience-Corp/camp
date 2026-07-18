//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A freshly created/adopted design workitem must be resolvable by its WI-<ref>
// selector, not just by path/slug. This is the WI-<ref> selector fix: the
// worktree link and the resulting commit tag must both work when the selector
// is the ref.
func TestIntegration_WorktreeAdd_ResolvesByRef(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/worktree-add-by-ref"
	initCommitTagsCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "sync-transport")

	_, err := tc.RunCampInDir(dir, "project", "new", "demo-app")
	require.NoError(t, err, "camp project new")

	out, err := tc.RunCampInDir(dir, "project", "worktree", "add", "sync-wt",
		"--project", "demo-app", "--workitem", ref)
	require.NoError(t, err, "worktree add --workitem <ref>: %s", out)

	wtRel := "projects/worktrees/demo-app/sync-wt"
	exists, err := tc.CheckDirExists(dir + "/" + wtRel)
	require.NoError(t, err)
	require.True(t, exists, "worktree dir should exist at %s; output:\n%s", wtRel, out)

	require.NoError(t, tc.WriteFile(dir+"/"+wtRel+"/foo.go", "package x\n"))
	commitOut, err := tc.RunCampInDir(dir+"/"+wtRel, "p", "commit", "-m", "feat: stub")
	require.NoError(t, err, "camp p commit inside worktree: %s", commitOut)

	subject := lastCommitSubject(t, tc, dir+"/"+wtRel)
	assert.Contains(t, subject, ref,
		"commit in a worktree linked via WI-<ref> should carry the ref: %s", subject)
}

// Linking an unadopted design directory (a directory under workflow/design with
// no .workitem marker) must fail fast with a clear "not adopted" error, and
// must NOT leave a dangling worktree behind.
func TestIntegration_WorktreeAdd_UnadoptedDesignDirErrors(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/worktree-add-unadopted"
	initCommitTagsCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "project", "new", "demo-app")
	require.NoError(t, err, "camp project new")

	// A design directory with content but no .workitem marker: discovered by
	// location, but has no stable id or ref.
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/loose/README.md", "# loose\n"))

	out, err := tc.RunCampInDir(dir, "project", "worktree", "add", "loose-wt",
		"--project", "demo-app", "--workitem", "workflow/design/loose")
	require.Error(t, err, "must reject an unadopted design dir; output:\n%s", out)
	assert.Contains(t, out, "not adopted", "error should name the not-adopted cause: %s", out)
	assert.Contains(t, out, "camp workitem adopt", "error should name the adopt command: %s", out)

	exists, err := tc.CheckDirExists(dir + "/projects/worktrees/demo-app/loose-wt")
	require.NoError(t, err)
	assert.False(t, exists, "a rejected link must not leave a dangling worktree behind")
}

// git worktree remove keeps the branch it created; re-adding a worktree with
// the same name must surface an actionable hint (naming the leftover branch and
// the --branch recovery) instead of a raw git "fatal: a branch named ... already
// exists". The --branch recovery it suggests must then succeed.
func TestIntegration_WorktreeAdd_LeftoverBranchHint(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/worktree-add-leftover-branch"
	initCommitTagsCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "project", "new", "demo-app")
	require.NoError(t, err, "camp project new")

	_, err = tc.RunCampInDir(dir, "project", "worktree", "add", "reuse-wt", "--project", "demo-app")
	require.NoError(t, err, "first worktree add")

	_, err = tc.RunCampInDir(dir, "project", "worktree", "remove", "reuse-wt", "--project", "demo-app")
	require.NoError(t, err, "worktree remove")

	out, err := tc.RunCampInDir(dir, "project", "worktree", "add", "reuse-wt", "--project", "demo-app")
	require.Error(t, err, "re-add over a leftover branch must error; output:\n%s", out)
	assert.Contains(t, out, "already exists", "error should name the leftover branch: %s", out)
	assert.Contains(t, out, "--branch", "error should point at the --branch recovery: %s", out)

	recoverOut, err := tc.RunCampInDir(dir, "project", "worktree", "add", "reuse-wt",
		"--project", "demo-app", "--branch", "reuse-wt")
	require.NoError(t, err, "recovery via --branch should succeed: %s", recoverOut)

	exists, err := tc.CheckDirExists(dir + "/projects/worktrees/demo-app/reuse-wt")
	require.NoError(t, err)
	assert.True(t, exists, "recovered worktree should exist")
}
