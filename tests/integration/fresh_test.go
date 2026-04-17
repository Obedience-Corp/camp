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

func runContainerShell(t *testing.T, tc *TestContainer, script string) string {
	t.Helper()

	output, exitCode, err := tc.ExecCommand("sh", "-lc", script)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "shell command failed:\n%s", output)
	return output
}

func gitOutput(t *testing.T, tc *TestContainer, dir string, args ...string) string {
	t.Helper()

	cmd := append([]string{"git", "-C", dir}, args...)
	output, exitCode, err := tc.ExecCommand(cmd...)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "git %v failed:\n%s", args, output)
	return strings.TrimSpace(output)
}

func setupFreshCampaignWithSubmodule(t *testing.T, tc *TestContainer, name string) (string, string, string) {
	t.Helper()

	campaignPath := "/campaigns/" + name
	bareDir := "/test/" + name + "-origin.git"
	seedDir := "/test/" + name + "-seed"

	runContainerShell(t, tc, fmt.Sprintf(`
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

	runContainerShell(t, tc, fmt.Sprintf(`
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

	runContainerShell(t, tc, fmt.Sprintf(`
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

	runContainerShell(t, tc, fmt.Sprintf(`
set -e
cd %[1]s
GIT_ALLOW_PROTOCOL=file git submodule add %[2]s projects/test-project
git commit -m 'Add monorepo project'
GIT_ALLOW_PROTOCOL=file git -C %[1]s/projects/test-project submodule update --init --recursive
`, campaignPath, projectBare))

	return campaignPath, campaignPath + "/projects/test-project", campaignPath + "/projects/test-project/vendor/tool"
}

func TestFresh_CreatesAndPushesNewBranch(t *testing.T) {
	tc := GetSharedContainer(t)
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, "fresh-create-push")

	output, err := tc.RunCampInDir(projectDir, "fresh", "--branch", "feat/new-work")
	require.NoError(t, err, "camp fresh should create and push a new branch:\n%s", output)

	current := gitOutput(t, tc, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "feat/new-work", current)

	upstream := gitOutput(t, tc, projectDir, "rev-parse", "--abbrev-ref", "@{upstream}")
	assert.Equal(t, "origin/feat/new-work", upstream)
}

func TestFresh_DoesNotPushExistingBranch(t *testing.T) {
	tc := GetSharedContainer(t)
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, "fresh-existing-branch")

	runContainerShell(t, tc, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b develop
printf 'develop\n' > %[1]s/develop.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Develop work'
git -C %[1]s checkout main
`, projectDir))

	output, err := tc.RunCampInDir(projectDir, "fresh", "--branch", "develop")
	require.NoError(t, err, "camp fresh should not push an existing branch:\n%s", output)

	current := gitOutput(t, tc, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "main", current)

	upstreamOutput, exitCode, err := tc.ExecCommand("git", "-C", projectDir, "rev-parse", "--abbrev-ref", "develop@{upstream}")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "existing develop branch should remain without an upstream: %s", upstreamOutput)
}

func TestFresh_HandlesDefaultBranchInAnotherWorktree(t *testing.T) {
	tc := GetSharedContainer(t)
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, "fresh-default-elsewhere")

	runContainerShell(t, tc, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feature-merged
printf 'feature\n' > %[1]s/feature.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Feature work'
`, projectDir))

	mainWorktree := "/test/fresh-default-elsewhere-main"
	stableWorktree := "/test/fresh-default-elsewhere-stable"
	mergedSiblingWorktree := "/test/fresh-default-elsewhere-sidecar"

	runContainerShell(t, tc, fmt.Sprintf(`
set -e
rm -rf %[2]s %[3]s %[4]s
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

	current := gitOutput(t, tc, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
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

	worktrees := gitOutput(t, tc, projectDir, "worktree", "list", "--porcelain")
	assert.Contains(t, worktrees, mainWorktree)
	assert.Contains(t, worktrees, stableWorktree)
	assert.NotContains(t, worktrees, mergedSiblingWorktree)
}

func TestFresh_RemovesOnlyMergedDetachedWorktrees(t *testing.T) {
	tc := GetSharedContainer(t)
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, "fresh-detached-cleanup")

	runContainerShell(t, tc, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feature-merged
printf 'feature\n' > %[1]s/feature.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Feature work'
`, projectDir))

	mainWorktree := "/test/fresh-detached-main"
	mergedDetached := "/test/fresh-detached-merged"
	unmergedDetached := "/test/fresh-detached-unmerged"

	runContainerShell(t, tc, fmt.Sprintf(`
set -e
rm -rf %[2]s %[3]s %[4]s
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

	current := gitOutput(t, tc, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "develop", current)

	worktrees := gitOutput(t, tc, projectDir, "worktree", "list", "--porcelain")
	assert.NotContains(t, worktrees, mergedDetached, "merged detached worktree should be removed")
	assert.Contains(t, worktrees, unmergedDetached, "unmerged detached worktree should remain")

	_, exitCode, err := tc.ExecCommand("test", "-d", unmergedDetached)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "unmerged detached worktree directory should remain")
}

func TestFresh_KeepsDirtyDetachedWorktrees(t *testing.T) {
	tc := GetSharedContainer(t)
	_, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, "fresh-detached-dirty")

	runContainerShell(t, tc, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feature-merged
printf 'feature\n' > %[1]s/feature.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Feature work'
`, projectDir))

	mainWorktree := "/test/fresh-detached-dirty-main"
	dirtyDetached := "/test/fresh-detached-dirty-review"

	runContainerShell(t, tc, fmt.Sprintf(`
set -e
rm -rf %[2]s %[3]s
git -C %[1]s worktree add %[2]s main
git -C %[1]s worktree add --detach %[3]s feature-merged
printf 'dirty\n' > %[3]s/dirty.txt
git -C %[2]s merge feature-merged
git -C %[2]s push origin main
`, projectDir, mainWorktree, dirtyDetached))

	output, err := tc.RunCampInDir(projectDir, "fresh", "--branch", "develop", "--no-push")
	require.NoError(t, err, "camp fresh should keep dirty detached worktrees:\n%s", output)

	current := gitOutput(t, tc, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "develop", current)

	worktrees := gitOutput(t, tc, projectDir, "worktree", "list", "--porcelain")
	assert.Contains(t, worktrees, dirtyDetached, "dirty detached worktree should remain")

	_, exitCode, err := tc.ExecCommand("test", "-d", dirtyDetached)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "dirty detached worktree directory should remain")
}

func TestFresh_IgnoresNestedSubmoduleRefDrift(t *testing.T) {
	tc := GetSharedContainer(t)
	_, projectDir, nestedDir := setupFreshCampaignWithNestedSubmoduleProject(t, tc, "fresh-nested-drift")

	runContainerShell(t, tc, fmt.Sprintf(`
set -e
git -C %[1]s config user.email test@test.com
git -C %[1]s config user.name Test
printf 'drift\n' > %[1]s/drift.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Nested drift'
`, nestedDir))

	status := gitOutput(t, tc, projectDir, "status", "--short", "--ignore-submodules=none")
	assert.Contains(t, status, "vendor/tool", "nested submodule drift should be visible before fresh runs")

	output, err := tc.RunCampInDir(projectDir, "fresh", "--branch", "develop", "--no-push", "--no-prune")
	require.NoError(t, err, "camp fresh should ignore nested submodule ref drift:\n%s", output)

	current := gitOutput(t, tc, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "develop", current)
}

func TestFresh_DoesNotDeleteRemoteBranches(t *testing.T) {
	tc := GetSharedContainer(t)
	_, projectDir, bareDir := setupFreshCampaignWithSubmodule(t, tc, "fresh-remote-branch")

	runContainerShell(t, tc, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feature-remote
printf 'feature remote\n' > %[1]s/feature-remote.txt
git -C %[1]s add .
git -C %[1]s commit -m 'Feature remote work'
git -C %[1]s push -u origin feature-remote
`, projectDir))

	mainWorktree := "/test/fresh-remote-main"
	runContainerShell(t, tc, fmt.Sprintf(`
set -e
rm -rf %[2]s
git -C %[1]s worktree add %[2]s main
git -C %[2]s merge feature-remote
git -C %[2]s push origin main
`, projectDir, mainWorktree))

	output, err := tc.RunCampInDir(projectDir, "fresh", "--no-branch", "--no-push")
	require.NoError(t, err, "camp fresh should not delete remote branches:\n%s", output)

	remoteHeads := gitOutput(t, tc, bareDir, "show-ref", "--verify", "refs/heads/feature-remote")
	assert.Contains(t, remoteHeads, "refs/heads/feature-remote")
}
