//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createBackstopWorkitem creates a design workitem at the campaign root and
// commits it, returning the id used at creation.
func createBackstopWorkitem(t *testing.T, tc *TestContainer, campaignPath, slug string) string {
	t.Helper()
	id := "design-" + slug
	out, err := tc.RunCampInDir(campaignPath,
		"workitem", "create", slug, "--type", "design", "--title", slug, "--id", id)
	require.NoError(t, err, "workitem create: %s", out)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git add -A && git commit -q -m 'add workitem'")
	require.NoError(t, err)
	return id
}

// mergeAndPruneBranch creates branch in the submodule, merges it into main on
// origin, and leaves it locally merged so camp fresh's prune deletes it.
func mergeAndPruneBranch(t *testing.T, tc *TestContainer, projectDir, branch string) {
	t.Helper()
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b %[2]s
printf 'work\n' > %[1]s/%[2]s.txt
git -C %[1]s add .
git -C %[1]s commit -m 'work on %[2]s'
git -C %[1]s checkout main
git -C %[1]s merge --no-ff -m 'merge %[2]s' %[2]s
git -C %[1]s push origin main
`, projectDir, branch))
}

// TestIntegration_FreshMergedBackstop_WorktreeLinkReported verifies the full
// wiring: a pruned branch whose name matches a worktree-scope link's directory
// basename is reported (merged_workitems: report) with the exact promote
// command, and the workitem is not moved (inference evidence never auto-acts).
func TestIntegration_FreshMergedBackstop_WorktreeLinkReported(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, "backstop-worktree")

	createBackstopWorkitem(t, tc, campaignPath, "myfeature")
	// Worktree-scope link whose path basename is the branch name. The link
	// command validates the target exists, so create the directory first.
	require.NoError(t, tc.WriteFile(campaignPath+"/projects/worktrees/test-project/myfeature/.keep", ""))
	out, err := tc.RunCampInDir(campaignPath, "workitem", "link", "design-myfeature", "--worktree", "projects/worktrees/test-project/myfeature")
	require.NoError(t, err, "link: %s", out)

	require.NoError(t, tc.WriteFile(campaignPath+"/.campaign/settings/fresh.yaml", "merged_workitems: \"report\"\n"))
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git add -A && git commit -q -m 'link + report mode'")
	require.NoError(t, err)

	mergeAndPruneBranch(t, tc, projectDir, "myfeature")

	output, err := tc.RunCampInDir(campaignPath, "fresh", "test-project", "--no-push")
	require.NoError(t, err, "fresh: %s", output)

	assert.Contains(t, output, "had a merged branch and is still active",
		"the merged worktree-linked branch should be reported:\n%s", output)
	assert.Contains(t, output, "camp workitem promote design-myfeature --target completed",
		"report should print the exact promote command:\n%s", output)

	// Inference evidence: the workitem must NOT have been moved.
	stays, err := tc.CheckDirExists(campaignPath + "/workflow/design/myfeature")
	require.NoError(t, err)
	assert.True(t, stays, "report mode must not move the workitem")
}

// TestIntegration_FreshMergedBackstop_PromptFallsBackToReportOnNonTTY verifies
// the default (prompt) reports rather than prompts/promotes on a non-TTY run,
// so agents never get an auto-promote path (FESTIVAL_RULES rule 2).
func TestIntegration_FreshMergedBackstop_PromptFallsBackToReportOnNonTTY(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, "backstop-prompt-nontty")

	createBackstopWorkitem(t, tc, campaignPath, "myfeature")
	require.NoError(t, tc.WriteFile(campaignPath+"/projects/worktrees/test-project/myfeature/.keep", ""))
	out, err := tc.RunCampInDir(campaignPath, "workitem", "link", "design-myfeature", "--worktree", "projects/worktrees/test-project/myfeature")
	require.NoError(t, err, "link: %s", out)
	// No fresh.yaml written: merged_workitems defaults to "prompt".
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git add -A && git commit -q -m 'link'")
	require.NoError(t, err)

	mergeAndPruneBranch(t, tc, projectDir, "myfeature")

	output, err := tc.RunCampInDir(campaignPath, "fresh", "test-project", "--no-push")
	require.NoError(t, err, "fresh: %s", output)

	assert.Contains(t, output, "promote when done", "default prompt must fall back to report on a non-TTY:\n%s", output)
	assert.Contains(t, output, "camp workitem promote design-myfeature --target completed")
	stays, err := tc.CheckDirExists(campaignPath + "/workflow/design/myfeature")
	require.NoError(t, err)
	assert.True(t, stays, "non-TTY prompt must not auto-promote")
}

// TestIntegration_FreshMergedBackstop_OffIsSilent verifies merged_workitems: off
// produces no backstop output even when a linked branch merged.
func TestIntegration_FreshMergedBackstop_OffIsSilent(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, "backstop-off")

	createBackstopWorkitem(t, tc, campaignPath, "myfeature")
	require.NoError(t, tc.WriteFile(campaignPath+"/projects/worktrees/test-project/myfeature/.keep", ""))
	out, err := tc.RunCampInDir(campaignPath, "workitem", "link", "design-myfeature", "--worktree", "projects/worktrees/test-project/myfeature")
	require.NoError(t, err, "link: %s", out)
	require.NoError(t, tc.WriteFile(campaignPath+"/.campaign/settings/fresh.yaml", "merged_workitems: \"off\"\n"))
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git add -A && git commit -q -m 'off mode'")
	require.NoError(t, err)

	mergeAndPruneBranch(t, tc, projectDir, "myfeature")

	output, err := tc.RunCampInDir(campaignPath, "fresh", "test-project", "--no-push")
	require.NoError(t, err, "fresh: %s", output)
	assert.NotContains(t, output, "had a merged branch", "off mode must produce no backstop output:\n%s", output)
}

// TestIntegration_FreshMergedBackstop_NoMatchIsSilent verifies a pruned branch
// with neither a worktree link nor a WI-tagged commit produces no report.
func TestIntegration_FreshMergedBackstop_NoMatchIsSilent(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	campaignPath, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, "backstop-nomatch")

	createBackstopWorkitem(t, tc, campaignPath, "unrelated")
	require.NoError(t, tc.WriteFile(campaignPath+"/.campaign/settings/fresh.yaml", "merged_workitems: \"report\"\n"))
	_, _, err := tc.ExecCommand("sh", "-c", "cd "+campaignPath+" && git add -A && git commit -q -m 'report mode'")
	require.NoError(t, err)

	mergeAndPruneBranch(t, tc, projectDir, "some-branch")

	output, err := tc.RunCampInDir(campaignPath, "fresh", "test-project", "--no-push")
	require.NoError(t, err, "fresh: %s", output)
	assert.NotContains(t, output, "had a merged branch", "a branch matching no workitem must not be reported:\n%s", output)
}
