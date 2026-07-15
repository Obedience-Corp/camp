//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFreshConfigure_ShowAddRemoveRoundTrip exercises the non-interactive
// `camp fresh configure` subcommand group end to end: an empty campaign
// shows nothing configured, add registers a step, a duplicate add is
// rejected, remove of an unknown name lists the valid ones, and remove of a
// real name restores the empty state.
func TestFreshConfigure_ShowAddRemoveRoundTrip(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-configure-roundtrip"
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	output, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "show")
	require.NoError(t, err, "configure show on an empty campaign should succeed:\n%s", output)
	assert.Contains(t, output, "(none configured)")

	output, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "install", "--run", "npm install")
	require.NoError(t, err, "configure add should succeed:\n%s", output)
	assert.Contains(t, output, "install")

	output, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "show")
	require.NoError(t, err, "configure show after add should succeed:\n%s", output)
	assert.Contains(t, output, "install")
	assert.Contains(t, output, "npm install")

	output, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "install", "--run", "npm ci")
	require.Error(t, err, "configure add with a duplicate name should fail:\n%s", output)

	output, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "remove", "does-not-exist")
	require.Error(t, err, "configure remove with an unknown name should fail:\n%s", output)
	assert.Contains(t, output, "install", "error should list the valid follow-up names")

	output, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "remove", "install")
	require.NoError(t, err, "configure remove should succeed:\n%s", output)

	output, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "show")
	require.NoError(t, err, "configure show after remove should succeed:\n%s", output)
	assert.Contains(t, output, "(none configured)")
}

// TestFreshConfigure_AddRejectsEmptyRun verifies the CLI rejects an empty
// --run value rather than persisting an unusable step.
func TestFreshConfigure_AddRejectsEmptyRun(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-configure-empty-run"
	campaignPath, _, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	output, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "bad", "--run", "")
	require.Error(t, err, "configure add with an empty --run should fail:\n%s", output)

	output, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "show")
	require.NoError(t, err, "configure show should still succeed:\n%s", output)
	assert.Contains(t, output, "(none configured)", "the rejected step must not have been persisted")
}

// TestFreshFollowUp_RunsConfiguredStepsInOrder verifies `camp fresh` runs
// every configured global follow-up, in configuration order, after a
// successful sync cycle.
func TestFreshFollowUp_RunsConfiguredStepsInOrder(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-followup-order"
	campaignPath, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	_, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "step1", "--run", "echo step1 >> follow-up.log")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "step2", "--run", "echo step2 >> follow-up.log")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(projectDir, "fresh", "--no-push")
	require.NoError(t, err, "camp fresh should run configured follow-ups:\n%s", output)
	assert.Contains(t, output, "step1")
	assert.Contains(t, output, "step2")

	log, err := tc.ReadFile(projectDir + "/follow-up.log")
	require.NoError(t, err, "follow-up.log should have been created by the steps")
	assert.Equal(t, "step1\nstep2\n", log, "follow-ups must run in configured order")
}

// TestFreshFollowUp_NoFollowUpSkipsSteps verifies --no-follow-up skips every
// configured step without affecting the rest of the sync cycle.
func TestFreshFollowUp_NoFollowUpSkipsSteps(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-followup-skip"
	campaignPath, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	_, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "marker", "--run", "touch ran.marker")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(projectDir, "fresh", "--no-push", "--no-follow-up")
	require.NoError(t, err, "camp fresh --no-follow-up should still complete the sync cycle:\n%s", output)
	assert.NotContains(t, output, "Follow-ups", "no follow-up section should print when steps are skipped")

	_, exitCode, err := tc.ExecCommand("test", "-f", projectDir+"/ran.marker")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "--no-follow-up must not execute configured steps")
}

// TestFreshFollowUp_DryRunListsButDoesNotExecute verifies --dry-run previews
// configured follow-ups without running any of them.
func TestFreshFollowUp_DryRunListsButDoesNotExecute(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-followup-dryrun"
	campaignPath, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	_, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "install", "--run", "touch ran.marker")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(projectDir, "fresh", "--dry-run")
	require.NoError(t, err, "camp fresh --dry-run should succeed:\n%s", output)
	assert.Contains(t, output, "install", "dry-run preview should list the configured step by name")
	assert.Contains(t, output, "would run", "dry-run preview should mark the step as not executed")

	_, exitCode, err := tc.ExecCommand("test", "-f", projectDir+"/ran.marker")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "--dry-run must never execute follow-up commands")
}

// TestFreshFollowUp_ContinueOnErrorKeepsGoing verifies a failed step marked
// continue_on_error lets the remaining steps run and the overall cycle
// succeed.
func TestFreshFollowUp_ContinueOnErrorKeepsGoing(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-followup-continue"
	campaignPath, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	_, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "flaky", "--run", "exit 1", "--continue-on-error")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "after", "--run", "touch after.marker")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(projectDir, "fresh", "--no-push")
	require.NoError(t, err, "camp fresh should succeed when the failing step allows continuing:\n%s", output)

	_, exitCode, err := tc.ExecCommand("test", "-f", projectDir+"/after.marker")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "the step after a continue_on_error failure should still run")
}

// TestFreshFollowUp_StopsOnFailureWithoutContinueOnError verifies a failed
// step without continue_on_error aborts the remaining follow-ups and fails
// the overall command.
func TestFreshFollowUp_StopsOnFailureWithoutContinueOnError(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-followup-stop"
	campaignPath, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	_, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "flaky", "--run", "exit 1")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "after", "--run", "touch after.marker")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(projectDir, "fresh", "--no-push")
	require.Error(t, err, "camp fresh should fail when a follow-up fails without continue_on_error:\n%s", output)

	_, exitCode, err := tc.ExecCommand("test", "-f", projectDir+"/after.marker")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "a step after an unrecovered failure must not run")
}

// TestFreshFollowUp_ProjectOverrideReplacesGlobal verifies a per-project
// follow-up list entirely replaces the global list for that project, rather
// than merging with it.
func TestFreshFollowUp_ProjectOverrideReplacesGlobal(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-followup-override"
	campaignPath, projectDir, _ := setupFreshCampaignWithSubmodule(t, tc, name)

	_, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "global-step", "--run", "touch global.marker")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "project-step",
		"--run", "touch project.marker", "--project", "test-project")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(projectDir, "fresh", "--no-push")
	require.NoError(t, err, "camp fresh should succeed:\n%s", output)

	_, exitCode, err := tc.ExecCommand("test", "-f", projectDir+"/project.marker")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "the project-scoped follow-up should have run")

	_, exitCode, err = tc.ExecCommand("test", "-f", projectDir+"/global.marker")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "the project override should replace the global list, not merge with it")
}

// TestFreshFollowUp_ListBatchRunsFollowUpsPerProject verifies follow-ups are
// resolved and run for each project in a `camp fresh --list` batch, not just
// for a single-project invocation.
func TestFreshFollowUp_ListBatchRunsFollowUpsPerProject(t *testing.T) {
	skipIfShort(t)
	tc := GetSharedContainer(t)
	const name = "fresh-followup-batch"
	campaignPath, projectDirA, projectDirB := setupFreshCampaignWithTwoSubmodules(t, tc, name)

	_, err := tc.RunCampInDir(campaignPath, "fresh", "configure", "add", "marker", "--run", "touch batch.marker")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "fresh", "--list", "test-a,test-b", "--no-push")
	require.NoError(t, err, "camp fresh --list should run follow-ups for each project:\n%s", output)

	for _, dir := range []string{projectDirA, projectDirB} {
		_, exitCode, err := tc.ExecCommand("test", "-f", dir+"/batch.marker")
		require.NoError(t, err)
		assert.Equal(t, 0, exitCode, "follow-up should have run in %s", dir)
	}
}
