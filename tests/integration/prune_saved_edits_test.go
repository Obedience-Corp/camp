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

// addMergedWorktree merges a branch into main and gives it a worktree holding
// uncommitted edits of every kind: a modified file, a deleted file and an
// untracked file. The edits are aged past the recent-edit window unless the
// test re-touches them.
func addMergedWorktree(t *testing.T, tc *TestContainer, campPath, projPath, branch string) string {
	t.Helper()
	wt := campPath + "/projects/worktrees/proj/" + branch
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s config user.email test@test.com
git -C %[1]s config user.name Test
git -C %[1]s checkout -b %[3]s
printf 'one\n' > %[1]s/keep.txt
printf 'gone\n' > %[1]s/remove.txt
printf '%[3]s\n' > "%[1]s/$(printf '%%s' '%[3]s' | tr / -).txt"
git -C %[1]s add . && git -C %[1]s commit -m 'feature'
git -C %[1]s checkout main
git -C %[1]s merge --no-ff %[3]s -m 'Merge %[3]s'
git -C %[1]s worktree add %[2]s %[3]s
printf 'one\nedited\n' > %[2]s/keep.txt
rm %[2]s/remove.txt
printf 'new\n' > %[2]s/untracked.txt
touch -d "@$(( $(date +%%s) - 259200 ))" %[2]s/keep.txt %[2]s/untracked.txt %[2]s
`, projPath, wt, branch))
	return wt
}

func TestPrune_DirtyMergedWorktreeIsSavedRemovedAndRestored(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-save")
	wt := addMergedWorktree(t, tc, campPath, projPath, "saved-feature")

	dry, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, dry, "would save edits")
	exists, err := tc.CheckDirExists(wt)
	require.NoError(t, err)
	require.True(t, exists, "a dry run must not remove anything")
	assert.Empty(t, tc.GitOutput(t, projPath, "for-each-ref", "refs/camp/pruned/"), "a dry run must not save anything")

	out, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "edits saved")
	assert.Contains(t, out, "--restore saved-feature")

	exists, err = tc.CheckDirExists(wt)
	require.NoError(t, err)
	assert.False(t, exists, "the worktree should be removed once its edits are saved")
	assert.NotContains(t, tc.GitOutput(t, projPath, "branch"), "saved-feature")

	listed, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--saved")
	require.NoError(t, err)
	assert.Contains(t, listed, "saved-feature")

	_, err = tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--restore", "saved-feature")
	require.NoError(t, err)

	status := tc.GitOutput(t, wt, "status", "--porcelain")
	assert.Contains(t, status, "M keep.txt")
	assert.Contains(t, status, "D remove.txt")
	assert.Contains(t, status, "?? untracked.txt")
	content, err := tc.ReadFile(wt + "/keep.txt")
	require.NoError(t, err)
	assert.Equal(t, "one\nedited\n", content)
	assert.Equal(t, "saved-feature", tc.GitOutput(t, wt, "branch", "--show-current"))
	assert.Equal(t, "feature", tc.GitOutput(t, wt, "log", "-1", "--format=%s"), "the branch must be back at its old tip")
	assert.Empty(t, tc.GitOutput(t, projPath, "for-each-ref", "refs/camp/pruned/"), "restore should drop the saved ref")
}

func TestPrune_RestoreRefusesWhenTheBranchExists(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-restore-refuse")
	addMergedWorktree(t, tc, campPath, projPath, "taken")

	_, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)
	tc.Shell(t, fmt.Sprintf(`git -C %s branch taken main`, projPath))

	out, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--restore", "taken")
	require.Error(t, err)
	assert.Contains(t, out, "already exists")
	assert.NotEmpty(t, tc.GitOutput(t, projPath, "for-each-ref", "refs/camp/pruned/"), "a refused restore must keep the saved edits")

	_, err = tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--drop", "taken")
	require.NoError(t, err)
	assert.Empty(t, tc.GitOutput(t, projPath, "for-each-ref", "refs/camp/pruned/"))
}

func TestPrune_DirtyWorktreeKeptWhenSavingIsNotSafe(t *testing.T) {
	cases := map[string]struct {
		prepare string // shell run with %[1]s = worktree, %[2]s = project
		args    []string
		want    string
	}{
		"edited recently": {
			prepare: `touch %[1]s/keep.txt`,
			want:    "edited",
		},
		"keep-dirty flag": {
			args: []string{"--keep-dirty"},
			want: "dirty worktree",
		},
		"over the size limit": {
			prepare: `dd if=/dev/zero of=%[1]s/big.bin bs=1M count=51 && touch -d "@$(( $(date +%%s) - 259200 ))" %[1]s/big.bin`,
			want:    "save limit",
		},
		"locked": {
			prepare: `git -C %[2]s worktree lock %[1]s`,
			want:    "locked worktree",
		},
		"holds another repository": {
			prepare: `git init -q %[1]s/copy && printf 'x\n' > %[1]s/copy/file.txt && git -C %[1]s/copy add . && git -C %[1]s/copy -c user.name=t -c user.email=t@t commit -qm copy && touch -d "@$(( $(date +%%s) - 259200 ))" %[1]s/copy %[1]s/copy/file.txt`,
			want:    "another git repository",
		},
		"a file deleted just now": {
			prepare: `touch %[1]s`,
			want:    "edited",
		},
		"merge in progress": {
			prepare: `touch "$(git -C %[1]s rev-parse --git-path MERGE_HEAD)"`,
			want:    "merge in progress",
		},
	}
	i := 0
	for name, tc2 := range cases {
		i++
		campName := fmt.Sprintf("prune-keep-%d", i)
		t.Run(name, func(t *testing.T) {
			tc := GetSharedContainer(t)
			campPath, projPath, _ := setupPruneCampaign(t, tc, campName)
			wt := addMergedWorktree(t, tc, campPath, projPath, "kept-feature")
			if tc2.prepare != "" {
				tc.Shell(t, fmt.Sprintf(tc2.prepare, wt, projPath))
			}

			args := append([]string{"project", "prune", "--project", "proj", "--force"}, tc2.args...)
			out, err := tc.RunCampInDir(campPath, args...)
			require.NoError(t, err)
			assert.Contains(t, out, tc2.want)

			exists, err := tc.CheckDirExists(wt)
			require.NoError(t, err)
			assert.True(t, exists, "the worktree must be kept")
			assert.Contains(t, tc.GitOutput(t, projPath, "branch"), "kept-feature", "its branch must be kept")
			assert.Empty(t, tc.GitOutput(t, projPath, "for-each-ref", "refs/camp/pruned/"), "nothing should be saved for a kept worktree")
			content, err := tc.ReadFile(wt + "/keep.txt")
			require.NoError(t, err)
			assert.Equal(t, "one\nedited\n", content, "its edits must be untouched")
		})
	}
}

func TestPrune_MissingWorktreeFolderDropsTheRegistration(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-missing-wt")
	wt := addMergedWorktree(t, tc, campPath, projPath, "vanished")
	tc.Shell(t, "rm -rf "+wt)

	out, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "registration dropped")
	assert.NotContains(t, tc.GitOutput(t, projPath, "branch"), "vanished")
	assert.NotContains(t, tc.GitOutput(t, projPath, "worktree", "list"), "vanished")
}

func TestPrune_SavedEditsExpire(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-expire")
	tc.Shell(t, fmt.Sprintf(`
set -e
cd %[1]s
make_saved() {
  c=$(GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t GIT_COMMITTER_DATE="$2" \
    git commit-tree 'HEAD^{tree}' -p HEAD -m "camp prune: uncommitted edits from $1

Camp-Pruned-Branch: $1
Camp-Pruned-Worktree: /nowhere/$1")
  git update-ref "refs/camp/pruned/$1" "$c"
}
make_saved old-edits "@$(( $(date +%%s) - 45 * 86400 )) +0000"
make_saved new-edits "@$(( $(date +%%s) - 2 * 86400 )) +0000"
`, projPath))

	out, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "saved edits expired")

	refs := tc.GitOutput(t, projPath, "for-each-ref", "--format=%(refname)", "refs/camp/pruned/")
	assert.NotContains(t, refs, "old-edits")
	assert.Contains(t, refs, "new-edits")
}

// setupSquashMergedBranch pushes a branch, then lands it on origin/main from a
// second clone as a squash that also carries a later fix, and deletes the
// remote branch. The squash is not patch-equivalent to the branch, so only a
// merged pull request can show the branch is safe to delete.
func setupSquashMergedBranch(t *testing.T, tc *TestContainer, name, projPath, barePath, branch string) (tip string) {
	t.Helper()
	peer := "/test/" + name + "-peer"
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s config user.email test@test.com
git -C %[1]s config user.name Test
git -C %[1]s checkout -b %[4]s
printf 'one\n' > %[1]s/feature.txt
git -C %[1]s add . && git -C %[1]s commit -m 'feature'
git -C %[1]s push -u origin %[4]s
git -C %[1]s checkout main

git clone %[2]s %[3]s
git -C %[3]s config user.email peer@test.com
git -C %[3]s config user.name Peer
printf 'one\nreview fix\n' > %[3]s/feature.txt
git -C %[3]s add . && git -C %[3]s commit -m 'feature (#1)'
git -C %[3]s push origin main
git -C %[3]s push origin --delete %[4]s
git -C %[1]s pull --ff-only origin main
`, projPath, barePath, peer, branch))
	return tc.GitOutput(t, projPath, "rev-parse", branch)
}

func installFakeGHPRList(t *testing.T, tc *TestContainer, name, json string) string {
	t.Helper()
	dir := "/test/" + name + "-gh"
	require.NoError(t, tc.WriteFile(dir+"/gh", "#!/bin/sh\ncat <<'JSON'\n"+json+"\nJSON\n"))
	_, exitCode, err := tc.ExecCommand("chmod", "+x", dir+"/gh")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	return dir
}

func TestPrune_MergedPullRequestMakesAGoneBranchPrunable(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, barePath := setupPruneCampaign(t, tc, "prune-pr-merged")
	tip := setupSquashMergedBranch(t, tc, "prune-pr-merged", projPath, barePath, "squashed")

	// Without an answer from GitHub the branch is kept, as before.
	out, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "not merge-equivalent")
	assert.Contains(t, out, "merged pull requests were not checked")
	require.Contains(t, tc.GitOutput(t, projPath, "branch"), "squashed")

	gh := installFakeGHPRList(t, tc, "prune-pr-merged",
		fmt.Sprintf(`[{"headRefName":"squashed","headRefOid":"%s"}]`, tip))
	out, err = runCampWithPath(tc, campPath, gh, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)
	assert.NotContains(t, tc.GitOutput(t, projPath, "branch"), "squashed", "output: %s", out)
}

func TestPrune_MergedPullRequestWithADifferentHeadKeepsTheBranch(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, barePath := setupPruneCampaign(t, tc, "prune-pr-ahead")
	setupSquashMergedBranch(t, tc, "prune-pr-ahead", projPath, barePath, "ahead")
	// The pull request merged at a commit this clone has never seen, so the
	// local branch may hold work the merge did not.
	gh := installFakeGHPRList(t, tc, "prune-pr-ahead",
		`[{"headRefName":"ahead","headRefOid":"1111111111111111111111111111111111111111"}]`)

	out, err := runCampWithPath(tc, campPath, gh, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "not merge-equivalent")
	assert.Contains(t, tc.GitOutput(t, projPath, "branch"), "ahead")
	assert.False(t, strings.Contains(out, "were not checked"), "gh answered, so no warning is expected: %s", out)
}

func TestPruneAll_AcceptsDryRun(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-all-dry")
	addMergedWorktree(t, tc, campPath, projPath, "all-feature")

	out, err := tc.RunCampInDir(campPath, "project", "prune", "all", "--dry-run")
	require.NoError(t, err, out)
	assert.Contains(t, out, "would save edits")
	assert.Contains(t, tc.GitOutput(t, projPath, "branch"), "all-feature")
}

// The confirmation must list what the pass will really do: a worktree whose
// edits will be saved says so, and a worktree that will be kept is not offered
// for deletion at all.
func TestPrune_PromptListsOnlyWhatWillHappen(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-prompt")
	saved := addMergedWorktree(t, tc, campPath, projPath, "will-save")
	kept := addMergedWorktree(t, tc, campPath, projPath, "will-keep")
	tc.Shell(t, "touch "+kept+"/keep.txt")

	out, err := tc.RunCampInteractiveInDir(campPath, "Proceed?", "n\n", "project", "prune", "--project", "proj")
	require.NoError(t, err, out)

	prompt, _, _ := strings.Cut(out, "Proceed?")
	assert.Contains(t, prompt, "Will delete 1 merged")
	assert.Contains(t, prompt, "will-save")
	assert.Contains(t, prompt, "uncommitted edits will be saved")
	assert.NotContains(t, prompt, "will-keep", "a worktree that is kept must not be listed for deletion")
	assert.Contains(t, out, "cancelled by user")

	for _, wt := range []string{saved, kept} {
		exists, err := tc.CheckDirExists(wt)
		require.NoError(t, err)
		assert.True(t, exists, "answering no must remove nothing: %s", wt)
	}
	assert.Empty(t, tc.GitOutput(t, projPath, "for-each-ref", "refs/camp/pruned/"))
}

func TestPrune_SavedEditsRoundTripForASlashedBranch(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-save-slash")
	wt := addMergedWorktree(t, tc, campPath, projPath, "feat/nested-name")

	out, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "--restore feat/nested-name")

	listed, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--saved")
	require.NoError(t, err)
	assert.Contains(t, listed, "feat/nested-name")

	_, err = tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--restore", "feat/nested-name")
	require.NoError(t, err)
	assert.Equal(t, "feat/nested-name", tc.GitOutput(t, wt, "branch", "--show-current"))
	assert.Contains(t, tc.GitOutput(t, wt, "status", "--porcelain"), "?? untracked.txt")
}

// A linked worktree keeps its submodule repositories in its own git dir. A
// commit made there and never pushed exists nowhere else, so even a worktree
// that looks clean must be kept.
func TestPrune_WorktreeWithUnpushedSubmoduleCommitIsKept(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupPruneCampaign(t, tc, "prune-sub-unpushed")
	wt := campPath + "/projects/worktrees/proj/with-sub"
	tc.Shell(t, fmt.Sprintf(`
set -e
git init -q --bare /test/prune-sub-unpushed-lib.git
git clone -q /test/prune-sub-unpushed-lib.git /test/prune-sub-unpushed-lib
cd /test/prune-sub-unpushed-lib
git config user.email t@t && git config user.name T
printf 'lib\n' > lib.txt && git add . && git commit -qm lib && git branch -M main && git push -q origin main
git --git-dir /test/prune-sub-unpushed-lib.git symbolic-ref HEAD refs/heads/main
cd %[1]s
git config user.email t@t && git config user.name T
git -c protocol.file.allow=always submodule add -q /test/prune-sub-unpushed-lib.git lib
git commit -qm 'add lib'
git checkout -q -b with-sub
printf 'x\n' > x.txt && git add . && git commit -qm x
git checkout -q main
git merge -q --no-ff with-sub -m 'Merge with-sub'
git worktree add -q %[2]s with-sub
cd %[2]s
git -c protocol.file.allow=always submodule update -q --init
cd lib
git config user.email t@t && git config user.name T
git checkout -q -b local-only
printf 'unpushed\n' > only-here.txt && git add . && git commit -qm 'unpushed work'
git checkout -q --detach HEAD~1
`, projPath, wt))
	require.Empty(t, tc.GitOutput(t, wt, "status", "--porcelain"), "the fixture worktree must look clean")

	out, err := tc.RunCampInDir(campPath, "project", "prune", "--project", "proj", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "submodule commits on no remote")
	exists, err := tc.CheckDirExists(wt)
	require.NoError(t, err)
	assert.True(t, exists, "the worktree must be kept")
	assert.Contains(t, tc.GitOutput(t, projPath, "branch"), "with-sub")
}
