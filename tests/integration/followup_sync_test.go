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

// commit.sync_project_refs records a submodule's new HEAD in the campaign root
// after a project commit. Under deferral the project commit happens in a
// worker, so the pointer commit has to happen after it, in the root's lane,
// created by the worker that made the commit it points at.

// setupSubmoduleCampaign builds a campaign with one real submodule and a bare
// remote, which is the minimum shape a gitlink test needs.
func setupSubmoduleCampaign(t *testing.T, tc *TestContainer, name string) (campPath, projectRel string) {
	t.Helper()
	campPath = "/campaigns/" + name
	projectRel = "projects/widget"

	_, err := tc.InitCampaign(campPath, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		set -e
		mkdir -p /remotes
		git init -q --bare /remotes/%[2]s-widget.git
		rm -rf /tmp/%[2]s-widget
		git init -q /tmp/%[2]s-widget
		cd /tmp/%[2]s-widget
		printf 'widget\n' > README.md
		git add README.md
		git commit -q -m "widget initial"
		git remote add origin /remotes/%[2]s-widget.git
		git push -q origin HEAD

		cd %[1]s
		git -c protocol.file.allow=always submodule add -q /remotes/%[2]s-widget.git %[3]s
		git add .gitmodules %[3]s
		git commit -q -m "add widget submodule"
	`, campPath, name, projectRel))

	// A message writer, since the deferred path this exercises is -aw.
	tc.Shell(t, fmt.Sprintf(`
		mkdir -p /writers
		printf '#!/bin/sh\necho "feat: widget change"\n' > /writers/%[2]s.sh
		chmod +x /writers/%[2]s.sh
		cd %[1]s
		printf '\nhooks:\n  commit_message:\n    command: /writers/%[2]s.sh\n' >> .campaign/campaign.yaml
		git add .campaign/campaign.yaml
		git commit -q -m "configure the message writer"
	`, campPath, name))

	return campPath, projectRel
}

// gitlinkSHA returns the submodule pointer recorded in the root's HEAD.
func gitlinkSHA(t *testing.T, tc *TestContainer, campPath, projectRel string) string {
	t.Helper()
	out := tc.GitOutput(t, campPath, "ls-tree", "HEAD", "--", projectRel)
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 3 {
		return ""
	}
	return fields[2]
}

// Criterion 37f: a sync-enabled deferred project commit lands, and the campaign
// root then records the pointer the project commit created.
func TestIntegration_FollowUpSyncRecordsTheProjectsNewHead(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, projectRel := setupSubmoduleCampaign(t, tc, "followup-sync")

	pointerBefore := gitlinkSHA(t, tc, campPath, projectRel)
	require.NotEmpty(t, pointerBefore, "the submodule pointer must exist before the test acts")

	tc.Shell(t, fmt.Sprintf(`
		cd %s/%s
		printf 'a change\n' > change.md
	`, campPath, projectRel))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(
		campPath+"/"+projectRel, "p", "commit", "--auto-write", "--sync")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	require.Contains(t, stdout+stderr, "staged and queued",
		"this test is about the deferred path; output:\n%s", stdout+stderr)

	// Both lanes: the project's commit, then the root's pointer commit that the
	// worker enqueues only once the first has landed.
	drainJobs(t, tc, campPath)

	projectHead := strings.TrimSpace(
		tc.GitOutput(t, campPath+"/"+projectRel, "rev-parse", "HEAD"))
	assert.NotEqual(t, pointerBefore, projectHead,
		"the deferred project commit must have landed")

	pointerAfter := gitlinkSHA(t, tc, campPath, projectRel)
	assert.Equal(t, projectHead, pointerAfter,
		"the root must record the project's new HEAD; the follow-up did not run or recorded the wrong commit")

	rootSubject := strings.TrimSpace(tc.GitOutput(t, campPath, "log", "-1", "--format=%s"))
	assert.Contains(t, rootSubject, projectRel,
		"the pointer commit must name what it updated; got %q", rootSubject)
}

// Ordering is inherent rather than enforced: the follow-up does not exist until
// its prerequisite has landed, so it cannot run early.
func TestIntegration_FollowUpDoesNotExistBeforeItsParentLands(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, projectRel := setupSubmoduleCampaign(t, tc, "followup-order")

	// A wedged project lane: a fresh lock means the drain will not spawn a
	// worker, so the parent job cannot run.
	tc.Shell(t, fmt.Sprintf(`
		cd %s/%s
		printf 'held\n' > held.md
	`, campPath, projectRel))

	_, _, exitCode, err := tc.RunCampSplitInDir(
		campPath+"/"+projectRel, "p", "commit", "--auto-write", "--sync")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Before any worker runs, the root lane must be empty: the follow-up is
	// created by the worker, not by the enqueuer.
	assert.Zero(t, pendingJobCount(t, tc, campPath, "pending", rootLane),
		"the follow-up must not be queued until its parent has committed")

	drainJobs(t, tc, campPath)

	pointerAfter := gitlinkSHA(t, tc, campPath, projectRel)
	projectHead := strings.TrimSpace(
		tc.GitOutput(t, campPath+"/"+projectRel, "rev-parse", "HEAD"))
	assert.Equal(t, projectHead, pointerAfter,
		"once the parent lands, the follow-up must record its commit")
}

// Criterion 37l: the gitlink carve-out is narrow. Only a worker-created
// follow-up may commit a submodule pointer.
//
// A gitlink records whatever the submodule's HEAD is at execution time, which
// is not a snapshot the enqueuer chose, so an ordinary deferred job committing
// one would publish a pointer to work nobody decided to publish.
func TestIntegration_HandCraftedGitlinkJobIsRefused(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, projectRel := setupSubmoduleCampaign(t, tc, "followup-carveout")

	pointerBefore := gitlinkSHA(t, tc, campPath, projectRel)

	// Move the submodule's HEAD so a pointer commit would be a real change.
	tc.Shell(t, fmt.Sprintf(`
		cd %s/%s
		printf 'unpublished\n' > unpublished.md
		git add unpublished.md
		git commit -q -m "work nobody asked to publish"
	`, campPath, projectRel))

	// A job that is not a follow-up, naming the gitlink.
	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind":    "commit-paths",
		"repo":    ".",
		"paths":   []string{projectRel},
		"message": "sneak the submodule pointer in",
	})

	drainJobs(t, tc, campPath)

	assert.Equal(t, pointerBefore, gitlinkSHA(t, tc, campPath, projectRel),
		"a non-follow-up job committed a submodule pointer; the carve-out is not holding")
	assert.Equal(t, 1, pendingJobCount(t, tc, campPath, "failed", rootLane),
		"the refused job must be visible in failed/, not silently dropped")

	stdout, _, _, err := tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	assert.Contains(t, stdout, "failed",
		"the refusal must be visible to the user; camp jobs:\n%s", stdout)
}

// Criterion 37m: chaining is bounded at one level, refused both at enqueue and
// at execution. Each link would run against a repository further from the state
// its author saw.
func TestIntegration_NestedFollowUpIsRefusedAtExecution(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, projectRel := setupSubmoduleCampaign(t, tc, "followup-nested")

	// Hand-written, because enqueue validation would reject this shape: the
	// point is that a file which reached disk anyway does not execute.
	writeJob(t, tc, campPath, rootLane, 1, map[string]any{
		"kind":      "commit-paths",
		"repo":      ".",
		"paths":     []string{".campaign/intents/whatever.md"},
		"message":   "a follow-up carrying its own follow-up",
		"follow_up": true,
		"then": map[string]any{
			"kind":  "commit-paths",
			"repo":  ".",
			"paths": []string{projectRel},
		},
	})

	drainJobs(t, tc, campPath)

	assert.Equal(t, 1, pendingJobCount(t, tc, campPath, "failed", rootLane),
		"a nested follow-up must fail rather than execute")
	assert.Zero(t, pendingJobCount(t, tc, campPath, "pending", rootLane),
		"the refused job must not have spawned the chained one")
}

// CAMP_NO_DEFER runs the commit and the pointer sync inline, exactly as before
// deferral existed.
func TestIntegration_CampNoDeferSyncsInline(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projectRel := setupSubmoduleCampaign(t, tc, "followup-inline")

	tc.Shell(t, fmt.Sprintf(`
		cd %s/%s
		printf 'inline change\n' > inline.md
	`, campPath, projectRel))

	out := tc.Shell(t, fmt.Sprintf(
		"cd %s/%s && CAMP_NO_DEFER=1 /camp p commit --auto-write --sync 2>&1",
		campPath, projectRel))

	assert.NotContains(t, out, "staged and queued",
		"CAMP_NO_DEFER must commit inline; output:\n%s", out)

	projectHead := strings.TrimSpace(
		tc.GitOutput(t, campPath+"/"+projectRel, "rev-parse", "HEAD"))
	assert.Equal(t, projectHead, gitlinkSHA(t, tc, campPath, projectRel),
		"the inline path must sync the pointer before returning; output:\n%s", out)
}

// A deferred commit must end up with the same subject its synchronous twin
// would have. The campaign tag is resolved from the user's working directory,
// which a detached worker does not have.
func TestIntegration_DeferredCommitCarriesTheCampaignTag(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupSubmoduleCampaign(t, tc, "followup-tag")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'tagged\n' > tagged.md
	`, campPath))

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	drainJobs(t, tc, campPath)

	subject := strings.TrimSpace(tc.GitOutput(t, campPath, "log", "-1", "--format=%s"))
	assert.True(t, strings.HasPrefix(subject, "[followup-tag:"),
		"a deferred commit lost the campaign tag its synchronous twin carries; subject: %q", subject)
	assert.Contains(t, subject, "feat: widget change",
		"the writer's message must survive the tag; subject: %q", subject)
}
