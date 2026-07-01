//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntegration_CommitSucceedsWithGitignoredWorktrees(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/commit-worktrees-ignored"
	_, err := tc.InitCampaign(campaignDir, "Worktrees Ignored", "product")
	require.NoError(t, err)

	ignoreCheck := tc.Shell(t, fmt.Sprintf(`
		cd %s
		git check-ignore -q -- projects/worktrees && echo RULE_PRESENT
	`, campaignDir))
	require.Contains(t, ignoreCheck, "RULE_PRESENT",
		"camp init should have written the worktrees gitignore rule")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p projects/worktrees/feature-x
		printf 'wt content' > projects/worktrees/feature-x/file.txt
		printf 'root content' > root.txt
	`, campaignDir))

	output, err := tc.RunCampInDir(campaignDir, "commit", "-m", "root content")
	require.NoError(t, err,
		"camp commit must not fail when the worktrees dir is gitignored; output:\n%s", output)

	committed := strings.Fields(tc.GitOutput(t, campaignDir, "show", "--name-only", "--format=", "HEAD"))
	require.Contains(t, committed, "root.txt")
	for _, path := range committed {
		require.NotContains(t, path, "projects/worktrees")
	}
}

func TestIntegration_CommitExcludesWorktreesWithoutIgnoreRule(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/commit-worktrees-unignored"
	_, err := tc.InitCampaign(campaignDir, "Worktrees Unignored", "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		sed -i '/orktree/d' .gitignore
		git add .gitignore
		git commit -q -m 'drop worktrees ignore rule'
		mkdir -p projects/worktrees/feature-x
		printf 'wt content' > projects/worktrees/feature-x/file.txt
		printf 'root content' > root.txt
	`, campaignDir))

	output, err := tc.RunCampInDir(campaignDir, "commit", "-m", "root content")
	require.NoError(t, err, "camp commit should succeed; output:\n%s", output)

	committed := strings.Fields(tc.GitOutput(t, campaignDir, "show", "--name-only", "--format=", "HEAD"))
	require.Contains(t, committed, "root.txt")
	for _, path := range committed {
		require.NotContains(t, path, "projects/worktrees",
			"worktrees content must stay excluded from root commits even without an ignore rule")
	}

	status := tc.GitOutput(t, campaignDir, "status", "--porcelain")
	require.Contains(t, status, "?? projects/worktrees/",
		"worktrees content should remain untracked, not staged")
}

func TestIntegration_CommitSucceedsWithTrackedContentUnderIgnoredWorktrees(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/commit-worktrees-tracked"
	_, err := tc.InitCampaign(campaignDir, "Worktrees Tracked", "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p projects/worktrees/samantha
		touch projects/worktrees/.gitkeep
		git init -q projects/worktrees/samantha/wt-mod
		git -C projects/worktrees/samantha/wt-mod commit --allow-empty -qm c1
		git -c advice.addEmbeddedRepo=false add -f projects/worktrees/.gitkeep projects/worktrees/samantha/wt-mod
		git commit -qm 'track worktrees content'
		git -C projects/worktrees/samantha/wt-mod commit --allow-empty -qm c2
		mkdir -p projects/worktrees/samantha/review-pr
		echo z > projects/worktrees/samantha/review-pr/f.txt
		printf 'root content' > root.txt
	`, campaignDir))

	ignoreCheck := tc.Shell(t, fmt.Sprintf(`
		cd %s
		git check-ignore -q -- projects/worktrees || echo DIR_NOT_IGNORED
		git status --porcelain -- projects/worktrees
	`, campaignDir))
	require.Contains(t, ignoreCheck, "DIR_NOT_IGNORED",
		"tracked content must keep the worktrees dir itself unignored to model the regression")
	require.Contains(t, ignoreCheck, " M projects/worktrees/samantha/wt-mod")

	output, err := tc.RunCampInDir(campaignDir, "commit", "-m", "root content")
	require.NoError(t, err,
		"camp commit must not fail when the ignored worktrees dir contains tracked content; output:\n%s", output)

	committed := strings.Fields(tc.GitOutput(t, campaignDir, "show", "--name-only", "--format=", "HEAD"))
	require.Contains(t, committed, "root.txt")
	for _, path := range committed {
		require.NotContains(t, path, "projects/worktrees")
	}

	status := tc.GitOutput(t, campaignDir, "status", "--porcelain", "--", "projects/worktrees")
	require.Contains(t, status, "M projects/worktrees/samantha/wt-mod",
		"dirty worktree gitlink must remain unstaged, not committed or discarded")
	require.NotContains(t, status, "M  projects/worktrees/samantha/wt-mod",
		"dirty worktree gitlink must not be left staged")
	staged := strings.TrimSpace(tc.GitOutput(t, campaignDir, "diff", "--cached", "--name-only"))
	require.Empty(t, staged, "nothing should remain staged after camp commit")
}

func TestIntegration_StageExcludesWorktreesWithoutIgnoreRule(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/stage-worktrees-unignored"
	_, err := tc.InitCampaign(campaignDir, "Stage Worktrees Unignored", "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		sed -i '/orktree/d' .gitignore
		git add .gitignore
		git commit -q -m 'drop worktrees ignore rule'
		mkdir -p projects/worktrees/feature-x
		printf 'wt content' > projects/worktrees/feature-x/file.txt
		printf 'notes' > notes.md
	`, campaignDir))

	output, err := tc.RunCampInDir(campaignDir, "stage")
	require.NoError(t, err, "camp stage should succeed; output:\n%s", output)

	staged := strings.Fields(tc.GitOutput(t, campaignDir, "diff", "--cached", "--name-only"))
	require.Contains(t, staged, "notes.md")
	for _, path := range staged {
		require.NotContains(t, path, "projects/worktrees",
			"worktrees content must stay excluded from root staging even without an ignore rule")
	}
}
