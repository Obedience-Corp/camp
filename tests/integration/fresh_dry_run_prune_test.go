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

// A dry-run moves no local ref, so it has to preview the prune step from where
// the sync step would leave the project: on the default branch, level with
// origin. These tests stage the state a developer is in right after their pull
// requests merged (sitting on a feature branch, local main predating the
// merges) and require the preview to name exactly what the real run removes.

// setupFreshUpstreamMerges creates three pushed feature branches in projectDir
// and lands two of them on origin/main from a second clone, deleting their
// remote branches the way a merged pull request does:
//
//	feat-merged  merged with a merge commit (ancestry-merged)
//	feat-squash  squash-merged (only its upstream being gone gives it away)
//	feat-open    still open; never merged, remote branch kept
//
// projectDir is left on feat-squash with a main that predates both merges.
func setupFreshUpstreamMerges(t *testing.T, tc *TestContainer, name, projectDir, bareDir string) {
	t.Helper()
	peerDir := "/test/" + name + "-merger"
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feat-merged
printf 'merged\n' > %[1]s/merged.txt
git -C %[1]s add merged.txt
git -C %[1]s commit -m 'Merged feature'
git -C %[1]s push -u origin feat-merged

git -C %[1]s checkout main
git -C %[1]s checkout -b feat-squash
printf 'one\n' > %[1]s/squash.txt
git -C %[1]s add squash.txt
git -C %[1]s commit -m 'Squash part one'
printf 'two\n' >> %[1]s/squash.txt
git -C %[1]s commit -am 'Squash part two'
git -C %[1]s push -u origin feat-squash

git -C %[1]s checkout main
git -C %[1]s checkout -b feat-open
printf 'open\n' > %[1]s/open.txt
git -C %[1]s add open.txt
git -C %[1]s commit -m 'Open work'
git -C %[1]s push -u origin feat-open

git -C %[1]s checkout feat-squash

git clone %[2]s %[3]s
git -C %[3]s config user.email peer@test.com
git -C %[3]s config user.name Peer
git -C %[3]s merge --no-ff origin/feat-merged -m 'Merge feat-merged'
git -C %[3]s merge --squash origin/feat-squash
git -C %[3]s commit -m 'feat-squash (squashed)'
git -C %[3]s push origin main
git -C %[3]s push origin --delete feat-merged feat-squash
`, projectDir, bareDir, peerDir))
}

// freshPruneSection returns the part of camp fresh output that follows the
// prune row, so a branch name mentioned earlier ("currently on feat-squash")
// cannot satisfy an assertion about what the prune step listed.
func freshPruneSection(t *testing.T, output string) string {
	t.Helper()
	const marker = "Prune merged branches"
	idx := strings.LastIndex(output, marker)
	require.NotEqual(t, -1, idx, "output has no prune row:\n%s", output)
	return output[idx:]
}

func freshLocalHeads(t *testing.T, tc *TestContainer, projectDir string) string {
	t.Helper()
	return tc.GitOutput(t, projectDir, "for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads")
}

func TestFresh_DryRunPreviewsWhatARealRunPrunes(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-dry-run-prune"
	_, projectDir, bareDir := setupFreshCampaignWithSubmodule(t, tc, name)

	// A merged feature that still has a clean linked worktree: the real run
	// removes the worktree and then the branch.
	treeWorktree := worktreePath(name, "tree")
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add -b feat-tree %[2]s main
printf 'tree\n' > %[2]s/tree.txt
git -C %[2]s add tree.txt
git -C %[2]s commit -m 'Tree feature'
git -C %[2]s push -u origin feat-tree
`, projectDir, treeWorktree))

	setupFreshUpstreamMerges(t, tc, name, projectDir, bareDir)
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s fetch origin
git -C %[1]s merge --no-ff origin/feat-tree -m 'Merge feat-tree'
git -C %[1]s push origin main
git -C %[1]s push origin --delete feat-tree
`, "/test/"+name+"-merger"))

	headsBefore := freshLocalHeads(t, tc, projectDir)
	worktreesBefore := tc.GitOutput(t, projectDir, "worktree", "list", "--porcelain")

	preview, err := tc.RunCampInDir(projectDir, "fresh", "--dry-run", "--no-branch", "--no-push", "--no-follow-up")
	require.NoError(t, err, "camp fresh --dry-run should preview the prune:\n%s", preview)

	section := freshPruneSection(t, preview)
	assert.Contains(t, section, "would delete 3 branches, would remove 1 worktree",
		"preview must count what the real run removes:\n%s", preview)
	for _, branch := range []string{"feat-merged", "feat-squash", "feat-tree"} {
		assert.Contains(t, section, branch, "preview must name %s:\n%s", branch, preview)
	}
	assert.NotContains(t, section, "feat-open", "an unmerged branch must not be previewed:\n%s", preview)
	assert.NotContains(t, section, "nothing to prune")

	// The preview changed nothing locally.
	assert.Equal(t, headsBefore, freshLocalHeads(t, tc, projectDir), "dry-run must not move or delete local branches")
	assert.Equal(t, "feat-squash", tc.GitOutput(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD"))
	assert.Equal(t, worktreesBefore, tc.GitOutput(t, projectDir, "worktree", "list", "--porcelain"))
	assert.Empty(t, tc.GitOutput(t, projectDir, "status", "--porcelain"))

	output, err := tc.RunCampInDir(projectDir, "fresh", "--no-branch", "--no-push", "--no-follow-up")
	require.NoError(t, err, "camp fresh should prune what it previewed:\n%s", output)
	assert.Contains(t, freshPruneSection(t, output), "deleted 3 branches, removed 1 worktree")

	assert.Equal(t, "feat-open\nmain",
		tc.GitOutput(t, projectDir, "for-each-ref", "--format=%(refname:short)", "refs/heads"),
		"the real run must delete exactly the previewed branches")
	assert.NotContains(t, tc.GitOutput(t, projectDir, "worktree", "list", "--porcelain"), treeWorktree)
}

// When a dirty worktree holds the default branch, fresh syncs the project
// detached at origin/<default>. The base ref is already current in that mode,
// so this isolates the other half of the preview: the branch the project is
// sitting on is deleted by the real run and must be previewed too.
func TestFresh_DryRunPreviewIncludesCurrentBranchWhenSyncingDetached(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-dry-run-prune-detached"
	_, projectDir, bareDir := setupFreshCampaignWithSubmodule(t, tc, name)
	setupFreshUpstreamMerges(t, tc, name, projectDir, bareDir)

	mainWorktree := worktreePath(name, "main")
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s main
printf 'uncommitted\n' >> %[2]s/README.md
`, projectDir, mainWorktree))

	headsBefore := freshLocalHeads(t, tc, projectDir)

	preview, err := tc.RunCampInDir(projectDir, "fresh", "--dry-run", "--no-branch", "--no-push", "--no-follow-up")
	require.NoError(t, err, "camp fresh --dry-run should preview the prune:\n%s", preview)
	assert.Contains(t, preview, "origin/main (detached)")

	section := freshPruneSection(t, preview)
	assert.Contains(t, section, "would delete 2 branches", "preview must count what the real run removes:\n%s", preview)
	assert.NotContains(t, section, "worktree", "the project's own checkout is not a worktree to remove:\n%s", preview)
	for _, branch := range []string{"feat-merged", "feat-squash"} {
		assert.Contains(t, section, branch, "preview must name %s:\n%s", branch, preview)
	}
	assert.NotContains(t, section, "feat-open")
	assert.Equal(t, headsBefore, freshLocalHeads(t, tc, projectDir), "dry-run must not move or delete local branches")
	assert.Equal(t, "feat-squash", tc.GitOutput(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD"))

	output, err := tc.RunCampInDir(projectDir, "fresh", "--no-branch", "--no-push", "--no-follow-up")
	require.NoError(t, err, "camp fresh should prune what it previewed:\n%s", output)
	assert.Contains(t, freshPruneSection(t, output), "deleted 2 branches")
	assert.Equal(t, "feat-open\nmain",
		tc.GitOutput(t, projectDir, "for-each-ref", "--format=%(refname:short)", "refs/heads"),
		"the real run must delete exactly the previewed branches")
}
