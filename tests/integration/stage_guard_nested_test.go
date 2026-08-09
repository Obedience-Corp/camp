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

// The nested-repository guard. These tests exist because of a real failure:
// three review worktrees were checked out inside a product repo, a blanket
// `camp p commit` recorded them as gitlinks, and because none of them appeared
// in .gitmodules every later `git submodule update --init --recursive` in that
// repository exited 128. The repo built and tested fine; only its `just dev`
// recipe, which syncs submodules first, stopped working, so the breakage read
// as "the app will not launch" for eighteen days.
//
// TestIntegration_NestedRepoKeepsSubmoduleCommandsWorking is that scenario end
// to end and is the regression the rest of this file supports.

// nestedRepoAt creates a git repository with one commit at repoPath, the shape
// a review worktree or a stray clone arrives in.
func nestedRepoAt(t *testing.T, tc *TestContainer, repoPath string) {
	t.Helper()
	tc.Shell(t, fmt.Sprintf(`
		mkdir -p %[1]s
		cd %[1]s
		git init -q .
		git config user.email nested@example.com
		git config user.name nested
		printf 'inner content' > inner.txt
		git add .
		git -c user.email=nested@example.com -c user.name=nested commit -q -m "nested commit"
	`, repoPath))
}

// indexGitlinks returns the mode-160000 index entries, which is what an
// undeclared nested repository becomes if the guard misses it.
func indexGitlinks(t *testing.T, tc *TestContainer, campPath string) []string {
	t.Helper()
	out := tc.GitOutput(t, campPath, "ls-files", "--stage")
	var links []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "160000 ") {
			continue
		}
		if _, path, found := strings.Cut(line, "\t"); found {
			links = append(links, strings.TrimSpace(path))
		}
	}
	return links
}

// The regression. A nested repository is kept out of the commit, everything
// beside it still lands, and the repository's submodule commands keep working.
//
// The last assertion is the one that matters most: asserting only on the index
// would pass against a guard that excluded the gitlink but left .gitmodules
// inconsistent some other way, and it was `git submodule update` exiting
// non-zero, not the index, that the user actually experienced.
func TestIntegration_NestedRepoKeepsSubmoduleCommandsWorking(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-nested-basic")
	nestedRepoAt(t, tc, campPath+"/review/app-pr136")

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "ordinary content")
	require.NoError(t, err, "output:\n%s", output)

	// Said out loud, with the remedy in the same breath.
	assert.Contains(t, output, "review/app-pr136 is its own git repository")
	assert.Contains(t, output, "left out of this commit")
	assert.Contains(t, output, "git submodule add <url> review/app-pr136")

	assert.Empty(t, indexGitlinks(t, tc, campPath),
		"an undeclared gitlink reached the index")

	// The rest of the commit is untouched: excluding is not refusing.
	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "notes/one.md")
	assert.NotContains(t, committed, "review/app-pr136")

	// The actual user-visible symptom, asserted directly.
	_, exitCode, err := tc.ExecCommand("git", "-C", campPath,
		"submodule", "update", "--init", "--recursive")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode,
		"git submodule update must still succeed after the commit")
}

// The nested repository keeps its own history. Excluding costs the user
// nothing, which is the reason this guard excludes rather than blocking.
func TestIntegration_NestedRepoRetainsItsOwnHistory(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-nested-history")
	nestedRepoAt(t, tc, campPath+"/review/app-pr137")

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "ordinary content")
	require.NoError(t, err, "output:\n%s", output)

	inner := tc.GitOutput(t, campPath+"/review/app-pr137", "log", "--oneline")
	assert.Contains(t, inner, "nested commit")
	exists, err := tc.CheckFileExists(campPath + "/review/app-pr137/inner.txt")
	require.NoError(t, err)
	assert.True(t, exists, "the nested checkout must be left on disk untouched")
}

// A submodule declared in .gitmodules is exactly the case where staging a
// gitlink is correct, so the guard must not fire on it. Without this the guard
// would break `camp project add`, which legitimately stages a new gitlink.
func TestIntegration_DeclaredSubmoduleIsNotFlagged(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-nested-declared")
	nestedRepoAt(t, tc, campPath+"/vendor/lib")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		cat > .gitmodules <<'EOF'
[submodule "vendor/lib"]
	path = vendor/lib
	url = git@example.com:demo/lib.git
EOF
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "with a declared submodule")
	require.NoError(t, err, "output:\n%s", output)

	assert.NotContains(t, output, "is its own git repository",
		"a declared submodule must not trip the nested-repository guard")
}

// --commit-nested is the user overruling the guard for one commit.
func TestIntegration_CommitNestedForcesTheGitlinkIn(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-nested-override")
	nestedRepoAt(t, tc, campPath+"/review/app-pr138")

	output, err := tc.RunCampInDir(campPath, "commit", "--commit-nested", "-m", "deliberate gitlink")
	require.NoError(t, err, "output:\n%s", output)

	assert.NotContains(t, output, "is its own git repository")
	assert.Contains(t, indexGitlinks(t, tc, campPath), "review/app-pr138")
}

// --commit-large must not quietly also authorize embedding a repository. The
// two flags answer unrelated questions, and a user who decided a build artifact
// belongs in git has not decided anything about nested checkouts.
func TestIntegration_CommitLargeDoesNotAuthorizeNestedRepos(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-nested-large-is-separate")
	nestedRepoAt(t, tc, campPath+"/review/app-pr139")

	output, err := tc.RunCampInDir(campPath, "commit", "--commit-large", "-m", "large ok, nested not")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "is its own git repository",
		"--commit-large must leave the nested-repository guard running")
	assert.Empty(t, indexGitlinks(t, tc, campPath))
}

// Block mode refuses the whole operation and stages nothing.
func TestIntegration_NestedRepoBlockModeStagesNothing(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-nested-block")
	nestedRepoAt(t, tc, campPath+"/review/app-pr140")
	writeGuardConfig(t, tc, campPath, "    nested_repos: block")

	output, _ := tc.RunCampInDir(campPath, "commit", "-m", "should refuse")

	assert.Contains(t, output, "not declared in .gitmodules would be committed")
	assert.Contains(t, output, "Nothing was staged")
	assert.Contains(t, output, "--commit-nested")
	assert.Empty(t, stagedPaths(t, tc, campPath), "block mode must stage nothing")
}

// Off disables the guard, which is the escape hatch for a workflow that
// deliberately carries undeclared gitlinks.
func TestIntegration_NestedRepoOffModeIsSilent(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-nested-off")
	nestedRepoAt(t, tc, campPath+"/review/app-pr141")
	writeGuardConfig(t, tc, campPath, "    nested_repos: off")

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "guard disabled")
	require.NoError(t, err, "output:\n%s", output)

	assert.NotContains(t, output, "is its own git repository")
	assert.Contains(t, indexGitlinks(t, tc, campPath), "review/app-pr141")
}

// A misspelled mode is an error rather than a silent fallback, so a typo never
// quietly leaves the user unprotected while they believe it is set.
func TestIntegration_NestedRepoInvalidModeIsReported(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-nested-invalid-mode")
	writeGuardConfig(t, tc, campPath, "    nested_repos: auto")

	output, _ := tc.RunCampInDir(campPath, "commit", "-m", "bad config")

	assert.Contains(t, output, "commit.guards.nested_repos")
	assert.Contains(t, output, "must be exclude, block, or off")
}

// The project scope reaches the same verdict as the campaign root. An
// undeclared gitlink breaks submodule commands identically in either, so unlike
// the large-file guard there is nothing here that depends on scope.
func TestIntegration_NestedRepoGuardAppliesInsideAProject(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-nested-project")

	projectPath := campPath + "/projects/api"
	require.NoError(t, tc.CreateGitRepo(projectPath))
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'api source' > main.go
	`, projectPath))
	nestedRepoAt(t, tc, projectPath+"/review/pr-1")

	output, err := tc.RunCampInDir(projectPath, "project", "commit", "-m", "api work")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "review/pr-1 is its own git repository")
	assert.Empty(t, indexGitlinks(t, tc, projectPath))
}
