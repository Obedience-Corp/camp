//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// worktreePath returns a structurally-unique worktree path for a given test
// fixture name + role. Uniqueness is derived from the test's name parameter
// (already thread through setup helpers) rather than from defensive rm -rf
// guards in the setup scripts, so two tests picking the same role string
// never collide without notice.
func worktreePath(name, role string) string {
	return "/test/" + name + "-" + role
}

func setupFreshCampaignWithSubmodule(t *testing.T, tc *TestContainer, name string) (string, string, string) {
	t.Helper()

	campaignPath := "/campaigns/" + name
	bareDir := "/test/" + name + "-origin.git"
	seedDir := "/test/" + name + "-seed"

	tc.Shell(t, fmt.Sprintf(`
set -e
git init --bare %[1]s
git clone %[1]s %[2]s
git -C %[2]s config user.email test@test.com
git -C %[2]s config user.name Test
printf '# Test Project\n' > %[2]s/README.md
git -C %[2]s add .
git -C %[2]s commit -m 'Initial commit'
git -C %[2]s branch -M main
git -C %[2]s push origin main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main
`, bareDir, seedDir))

	_, err := tc.InitCampaign(campaignPath, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
set -e
cd %[1]s
GIT_ALLOW_PROTOCOL=file git submodule add %[2]s projects/test-project
git commit -m 'Add submodule'
`, campaignPath, bareDir))

	return campaignPath, campaignPath + "/projects/test-project", bareDir
}

func setupFreshCampaignWithNestedSubmoduleProject(t *testing.T, tc *TestContainer, name string) (string, string, string) {
	t.Helper()

	campaignPath := "/campaigns/" + name
	nestedBare := "/test/" + name + "-nested.git"
	nestedSeed := "/test/" + name + "-nested-seed"
	projectBare := "/test/" + name + "-project.git"
	projectSeed := "/test/" + name + "-project-seed"

	tc.Shell(t, fmt.Sprintf(`
set -e
git init --bare %[1]s
git clone %[1]s %[2]s
git -C %[2]s config user.email test@test.com
git -C %[2]s config user.name Test
printf '# Nested Project\n' > %[2]s/README.md
git -C %[2]s add .
git -C %[2]s commit -m 'Initial nested commit'
git -C %[2]s branch -M main
git -C %[2]s push origin main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main

git init --bare %[3]s
git clone %[3]s %[4]s
git -C %[4]s config user.email test@test.com
git -C %[4]s config user.name Test
printf '# Monorepo Project\n' > %[4]s/README.md
git -C %[4]s add .
git -C %[4]s commit -m 'Initial project commit'
git -C %[4]s branch -M main
GIT_ALLOW_PROTOCOL=file git -C %[4]s submodule add %[1]s vendor/tool
git -C %[4]s commit -m 'Add nested submodule'
git -C %[4]s push origin main
git --git-dir %[3]s symbolic-ref HEAD refs/heads/main
`, nestedBare, nestedSeed, projectBare, projectSeed))

	_, err := tc.InitCampaign(campaignPath, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
set -e
cd %[1]s
GIT_ALLOW_PROTOCOL=file git submodule add %[2]s projects/test-project
git commit -m 'Add monorepo project'
GIT_ALLOW_PROTOCOL=file git -C %[1]s/projects/test-project submodule update --init --recursive
`, campaignPath, projectBare))

	return campaignPath, campaignPath + "/projects/test-project", campaignPath + "/projects/test-project/vendor/tool"
}

// setupFreshCampaignWithTwoSubmodules creates a campaign with two independent
// submodule projects (test-a, test-b), each with its own bare origin on main.
// Used to exercise the multi-project `camp fresh <a> <b>` batch path.
func setupFreshCampaignWithTwoSubmodules(t *testing.T, tc *TestContainer, name string) (string, string, string) {
	t.Helper()

	campaignPath := "/campaigns/" + name
	bareA := "/test/" + name + "-a-origin.git"
	seedA := "/test/" + name + "-a-seed"
	bareB := "/test/" + name + "-b-origin.git"
	seedB := "/test/" + name + "-b-seed"

	tc.Shell(t, fmt.Sprintf(`
set -e
for spec in "%[1]s %[2]s" "%[3]s %[4]s"; do
  set -- $spec
  git init --bare "$1"
  git clone "$1" "$2"
  git -C "$2" config user.email test@test.com
  git -C "$2" config user.name Test
  printf '# Test Project\n' > "$2"/README.md
  git -C "$2" add .
  git -C "$2" commit -m 'Initial commit'
  git -C "$2" branch -M main
  git -C "$2" push origin main
  git --git-dir "$1" symbolic-ref HEAD refs/heads/main
done
`, bareA, seedA, bareB, seedB))

	_, err := tc.InitCampaign(campaignPath, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
set -e
cd %[1]s
GIT_ALLOW_PROTOCOL=file git submodule add %[2]s projects/test-a
GIT_ALLOW_PROTOCOL=file git submodule add %[3]s projects/test-b
git commit -m 'Add submodules'
`, campaignPath, bareA, bareB))

	return campaignPath, campaignPath + "/projects/test-a", campaignPath + "/projects/test-b"
}

// skipIfShort skips container-backed tests under `go test -short`. Each test
// calls Reset() which wipes + reinitializes the full container fixture, so the
// suite is not cheap enough for short-mode runs.
func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping container-backed integration test in short mode")
	}
}

func TestFresh_CreatesAndPushesNewBranch(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-create-push"
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	output, err := tc.RunCampInDir(projectDir, "fresh", "--branch", "feat/new-work")
	require.NoError(t, err, "camp fresh should create and push a new branch:\n%s", output)

	current := tc.GitOutput(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "feat/new-work", current)

	upstream := tc.GitOutput(t, projectDir, "rev-parse", "--abbrev-ref", "@{upstream}")
	assert.Equal(t, "origin/feat/new-work", upstream)
}

func TestFresh_DoesNotPushExistingBranch(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-existing-branch"
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b develop
printf 'develop\n' > %[1]s/develop.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Develop work'
git -C %[1]s checkout main
`, projectDir))

	output, err := tc.RunCampInDir(projectDir, "fresh", "--branch", "develop")
	require.NoError(t, err, "camp fresh should not push an existing branch:\n%s", output)

	current := tc.GitOutput(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "main", current)

	upstreamOutput, exitCode, err := tc.ExecCommand("git", "-C", projectDir, "rev-parse", "--abbrev-ref", "develop@{upstream}")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "existing develop branch should remain without an upstream: %s", upstreamOutput)
}

func TestFresh_HandlesDefaultBranchInAnotherWorktree(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-default-elsewhere"
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feature-merged
printf 'feature\n' > %[1]s/feature.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Feature work'
`, projectDir))

	mainWorktree := worktreePath(name, "main")
	stableWorktree := worktreePath(name, "stable")
	mergedSiblingWorktree := worktreePath(name, "sidecar")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s main
git -C %[1]s worktree add -b stable-v0.1.2 %[3]s main
printf 'release\n' > %[3]s/release.txt
git -C %[3]s add .
git -C %[3]s commit -m 'Release branch work'
git -C %[1]s worktree add -b feature-sidecar %[4]s main
printf 'sidecar\n' > %[4]s/sidecar.txt
git -C %[4]s add .
git -C %[4]s commit -m 'Sidecar work'
git -C %[2]s merge feature-merged
git -C %[2]s merge feature-sidecar
git -C %[2]s push origin main
`, projectDir, mainWorktree, stableWorktree, mergedSiblingWorktree))

	output, err := tc.RunCampInDir(projectDir, "fresh", "--branch", "develop", "--no-push")
	require.NoError(t, err, "camp fresh should handle main being checked out elsewhere:\n%s", output)

	current := tc.GitOutput(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "develop", current)

	_, exitCode, err := tc.ExecCommand("git", "-C", projectDir, "rev-parse", "--verify", "--quiet", "refs/heads/feature-merged")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "merged feature branch should be deleted")

	_, exitCode, err = tc.ExecCommand("git", "-C", projectDir, "rev-parse", "--verify", "--quiet", "refs/heads/feature-sidecar")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "merged sibling worktree branch should be deleted")

	_, exitCode, err = tc.ExecCommand("git", "-C", projectDir, "rev-parse", "--verify", "--quiet", "refs/heads/stable-v0.1.2")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "stable worktree branch should remain")

	worktrees := tc.GitOutput(t, projectDir, "worktree", "list", "--porcelain")
	assert.Contains(t, worktrees, mainWorktree)
	assert.Contains(t, worktrees, stableWorktree)
	assert.NotContains(t, worktrees, mergedSiblingWorktree)
}

func TestFresh_RemovesOnlyMergedDetachedWorktrees(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-detached-cleanup"
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feature-merged
printf 'feature\n' > %[1]s/feature.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Feature work'
`, projectDir))

	mainWorktree := worktreePath(name, "main")
	mergedDetached := worktreePath(name, "merged-detached")
	unmergedDetached := worktreePath(name, "unmerged-detached")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s main
git -C %[1]s worktree add --detach %[3]s feature-merged
git -C %[1]s worktree add --detach %[4]s feature-merged
git -C %[4]s config user.email test@test.com
git -C %[4]s config user.name Test
printf 'draft\n' > %[4]s/draft.txt
git -C %[4]s add .
git -C %[4]s commit -m 'Detached draft work'
git -C %[2]s merge feature-merged
git -C %[2]s push origin main
`, projectDir, mainWorktree, mergedDetached, unmergedDetached))

	output, err := tc.RunCampInDir(projectDir, "fresh", "--branch", "develop", "--no-push")
	require.NoError(t, err, "camp fresh should prune only merged detached worktrees:\n%s", output)

	current := tc.GitOutput(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "develop", current)

	worktrees := tc.GitOutput(t, projectDir, "worktree", "list", "--porcelain")
	assert.NotContains(t, worktrees, mergedDetached, "merged detached worktree should be removed")
	assert.Contains(t, worktrees, unmergedDetached, "unmerged detached worktree should remain")

	_, exitCode, err := tc.ExecCommand("test", "-d", unmergedDetached)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "unmerged detached worktree directory should remain")
}

// TestFresh_RemovesMergedDetachedWorktreeAfterBranchDeleted covers the pruner
// path where the source branch is gone but the detached worktree's HEAD commit
// is still reachable from the sync base ref. The merged-detection logic must
// classify the worktree by commit ancestry, not by looking up the (now
// non-existent) branch name. Ported from the host-side suite deleted in this
// PR; the analogous case in TestFresh_RemovesOnlyMergedDetachedWorktrees keeps
// the source branch alive, so without this test that code path would regress
// silently.
func TestFresh_RemovesMergedDetachedWorktreeAfterBranchDeleted(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-detached-branch-deleted"
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feature-merged
printf 'feature\n' > %[1]s/feature.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Feature work'
`, projectDir))

	mainWorktree := worktreePath(name, "main")
	mergedDetached := worktreePath(name, "merged-detached")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s main
git -C %[1]s worktree add --detach %[3]s feature-merged
# Move HEAD off feature-merged so 'git branch -d' can delete it, then delete it
# before running fresh. The detached worktree is still at the feature commit.
git -C %[1]s checkout -b scratch-work
git -C %[2]s merge feature-merged
git -C %[2]s push origin main
git -C %[1]s branch -d feature-merged
`, projectDir, mainWorktree, mergedDetached))

	output, err := tc.RunCampInDir(projectDir, "fresh", "--no-push")
	require.NoError(t, err, "camp fresh should prune merged detached worktree even after source branch is gone:\n%s", output)

	worktrees := tc.GitOutput(t, projectDir, "worktree", "list", "--porcelain")
	assert.NotContains(t, worktrees, mergedDetached,
		"merged detached worktree should be removed when classified by commit ancestry, even if its source branch no longer exists")
}

func TestFresh_KeepsDirtyDetachedWorktrees(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-detached-dirty"
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feature-merged
printf 'feature\n' > %[1]s/feature.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Feature work'
`, projectDir))

	mainWorktree := worktreePath(name, "main")
	dirtyDetached := worktreePath(name, "dirty-detached")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s main
git -C %[1]s worktree add --detach %[3]s feature-merged
printf 'dirty\n' > %[3]s/dirty.txt
git -C %[2]s merge feature-merged
git -C %[2]s push origin main
`, projectDir, mainWorktree, dirtyDetached))

	output, err := tc.RunCampInDir(projectDir, "fresh", "--branch", "develop", "--no-push")
	require.NoError(t, err, "camp fresh should keep dirty detached worktrees:\n%s", output)

	current := tc.GitOutput(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "develop", current)

	worktrees := tc.GitOutput(t, projectDir, "worktree", "list", "--porcelain")
	assert.Contains(t, worktrees, dirtyDetached, "dirty detached worktree should remain")

	_, exitCode, err := tc.ExecCommand("test", "-d", dirtyDetached)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "dirty detached worktree directory should remain")
}

func TestFresh_IgnoresNestedSubmoduleRefDrift(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-nested-drift"
	_, projectDir, nestedDir := setupFreshCampaignWithNestedSubmoduleProject(t, tc, name)

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s config user.email test@test.com
git -C %[1]s config user.name Test
printf 'drift\n' > %[1]s/drift.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Nested drift'
`, nestedDir))

	status := tc.GitOutput(t, projectDir, "status", "--short", "--ignore-submodules=none")
	assert.Contains(t, status, "vendor/tool", "nested submodule drift should be visible before fresh runs")

	output, err := tc.RunCampInDir(projectDir, "fresh", "--branch", "develop", "--no-push", "--no-prune")
	require.NoError(t, err, "camp fresh should ignore nested submodule ref drift:\n%s", output)

	current := tc.GitOutput(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "develop", current)
}

// TestFresh_ListCyclesEachProject verifies `camp fresh --list a,b` runs the
// fresh cycle against each named project in a single invocation.
func TestFresh_ListCyclesEachProject(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-list-cycle"
	campaignPath, projectDirA, projectDirB := setupFreshCampaignWithTwoSubmodules(t, tc, name)

	output, err := tc.RunCampInDir(campaignPath, "fresh", "--list", "test-a,test-b", "--branch", "develop", "--no-push")
	require.NoError(t, err, "camp fresh --list should cycle each named project:\n%s", output)

	assert.Contains(t, output, "Running fresh across 2 project(s)",
		"batch run should announce the number of projects")

	currentA := tc.GitOutput(t, projectDirA, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "develop", currentA, "test-a should be cycled onto the new branch")

	currentB := tc.GitOutput(t, projectDirB, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "develop", currentB, "test-b should be cycled onto the new branch")
}

// TestFresh_ListFailFastOnUnknownName verifies that an unknown project name
// anywhere in --list aborts the batch before mutating any project, since all
// names are resolved up front.
func TestFresh_ListFailFastOnUnknownName(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-list-badname"
	campaignPath, projectDirA, _ := setupFreshCampaignWithTwoSubmodules(t, tc, name)

	output, err := tc.RunCampInDir(campaignPath, "fresh", "--list", "test-a,does-not-exist", "--branch", "develop", "--no-push")
	require.Error(t, err, "camp fresh --list should fail when a project name is unknown:\n%s", output)

	currentA := tc.GitOutput(t, projectDirA, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "main", currentA, "test-a must not be mutated when the batch fails name validation")

	_, exitCode, err := tc.ExecCommand("git", "-C", projectDirA, "rev-parse", "--verify", "--quiet", "refs/heads/develop")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "develop branch should not be created when the batch aborts on validation")
}

// TestFresh_ListRejectsPositionalArg verifies that combining --list with a
// positional project name is rejected rather than silently ignoring one.
func TestFresh_ListRejectsPositionalArg(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-list-conflict"
	campaignPath, projectDirA, _ := setupFreshCampaignWithTwoSubmodules(t, tc, name)

	output, err := tc.RunCampInDir(campaignPath, "fresh", "test-a", "--list", "test-b", "--no-push")
	require.Error(t, err, "camp fresh should reject a positional name combined with --list:\n%s", output)

	currentA := tc.GitOutput(t, projectDirA, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "main", currentA, "no project should be mutated when the invocation is rejected")
}

func TestFresh_DoesNotDeleteRemoteBranches(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-remote-branch"
	_, projectDir, bareDir := setupFreshCampaignWithSubmodule(t, tc, name)

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feature-remote
printf 'feature remote\n' > %[1]s/feature-remote.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Feature remote work'
git -C %[1]s push -u origin feature-remote
`, projectDir))

	mainWorktree := worktreePath(name, "main")
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s main
git -C %[2]s merge feature-remote
git -C %[2]s push origin main
`, projectDir, mainWorktree))

	output, err := tc.RunCampInDir(projectDir, "fresh", "--no-branch", "--no-push")
	require.NoError(t, err, "camp fresh should not delete remote branches:\n%s", output)

	remoteHeads := tc.GitOutput(t, bareDir, "show-ref", "--verify", "refs/heads/feature-remote")
	assert.Contains(t, remoteHeads, "refs/heads/feature-remote")
}

// TestFresh_ReclaimsDefaultBranchFromOtherWorktree proves that when main is
// checked out in a clean feature worktree (the common leftover after
// worktree add --start-point main), camp fresh detaches that worktree and
// checks out main on the primary project path instead of failing with
// "already used by worktree" or leaving the primary on detached origin/main.
func TestFresh_ReclaimsDefaultBranchFromOtherWorktree(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-reclaim-main"
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	// Primary starts on a feature branch; main is held by a finished worktree.
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feature-active
printf 'work\n' > %[1]s/feature.txt
git -C %[1]s add .
git -C %[1]s commit -m 'feature work'
`, projectDir))

	stuckWT := worktreePath(name, "stuck-main")
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s main
`, projectDir, stuckWT))

	// Confirm main is locked before fresh.
	branchOnStuck := tc.GitOutput(t, stuckWT, "rev-parse", "--abbrev-ref", "HEAD")
	require.Equal(t, "main", branchOnStuck, "precondition: stuck worktree should hold main")

	// --no-prune so the reclaimed worktree is not removed after detach
	// (prune deletes clean detached worktrees that match merged history).
	output, err := tc.RunCampInDir(projectDir, "fresh", "--no-branch", "--no-push", "--no-follow-up", "--no-prune")
	require.NoError(t, err, "camp fresh should reclaim main:\n%s", output)
	assert.Contains(t, output, "Free", "fresh should report freeing the default branch")
	assert.Contains(t, output, "stuck-main", "fresh should name the occupying worktree")

	// Primary is on main (not detached origin/main).
	primary := tc.GitOutput(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "main", primary, "primary project path should hold main after fresh")

	// Occupying worktree was detached so main is free for the primary path.
	stuck := tc.GitOutput(t, stuckWT, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "HEAD", stuck, "occupying worktree should be detached, got %s", stuck)
}
