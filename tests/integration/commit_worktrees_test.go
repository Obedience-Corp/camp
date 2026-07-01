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
