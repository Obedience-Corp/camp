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

// A deferred bookkeeping commit that names a directory must stage what
// `git add <dir>` would stage, and nothing else.
//
// The deferred path captures content itself instead of letting git resolve the
// paths later, which is what makes the commit mean its message. That capture
// therefore has to reproduce git's own answer. Listing the directory off the
// disk does not: the disk includes ignored files, and it cannot show a tracked
// file that is no longer there.

// setupIncludeCampaign builds a campaign with a workitem, so
// `camp workitem commit --include <path>` has something to attribute a commit
// to, and with an ignore rule to test against.
func setupIncludeCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()
	campPath, _ := setupDrainCampaign(t, tc, name)

	const marker = `version: v1alpha6
kind: workitem
id: design-capture-2026-08-05
type: design
title: Capture
ref: WI-cap123
`
	require.NoError(t, tc.WriteFile(campPath+"/workflow/design/capture/.workitem", marker))
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf '*.log\n' > .gitignore
		git add .gitignore workflow/design/capture/.workitem
		git commit -q -m "add an ignore rule and a workitem"
	`, campPath))
	return campPath
}

// headPaths returns every path in HEAD's tree.
func headPaths(t *testing.T, tc *TestContainer, campPath string) []string {
	t.Helper()
	out := tc.GitOutput(t, campPath, "ls-tree", "-r", "--name-only", "HEAD")
	var paths []string
	for line := range strings.SplitSeq(out, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// An ignored file inside a captured directory must not be committed. The
// synchronous path never staged it, so the deferred path committing it is a
// difference the user did not ask for and would not see until it was in the
// history.
func TestIntegration_DeferredDirectoryCaptureSkipsIgnoredFiles(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath := setupIncludeCampaign(t, tc, "capture-ignored")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p payload
		printf 'keep me\n' > payload/keep.md
		printf 'noisy output\n' > payload/debug.log
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath,
		"workitem", "commit", "--workitem", "capture", "--include", "payload", "-m", "capture payload")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	drainJobs(t, tc, campPath)

	paths := headPaths(t, tc, campPath)
	assert.Contains(t, paths, "payload/keep.md",
		"the non-ignored file must be committed; HEAD:\n%s", strings.Join(paths, "\n"))
	assert.NotContains(t, paths, "payload/debug.log",
		"an ignored file must not reach the commit; HEAD:\n%s", strings.Join(paths, "\n"))
}

// A file that is ignored but already tracked still belongs in the commit.
// Git keeps staging changes to it, because the ignore rule governs what gets
// added, not what stays tracked, and a capture that dropped it would silently
// discard a change the user made.
func TestIntegration_DeferredDirectoryCaptureKeepsTrackedIgnoredFiles(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath := setupIncludeCampaign(t, tc, "capture-tracked-ignored")

	// Force-added, so it is tracked despite matching *.log.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p payload
		printf 'original\n' > payload/tracked.log
		git add -f payload/tracked.log
		git commit -q -m "track a file that matches the ignore rule"
		printf 'edited\n' > payload/tracked.log
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath,
		"workitem", "commit", "--workitem", "capture", "--include", "payload", "-m", "capture payload")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	drainJobs(t, tc, campPath)

	content := tc.GitOutput(t, campPath, "show", "HEAD:payload/tracked.log")
	assert.Contains(t, content, "edited",
		"a tracked file must be captured even when it matches an ignore rule; got %q", content)
}

// A tracked file deleted from inside a captured directory must be recorded as
// the deletion it is. A disk listing cannot see it at all, so the commit
// silently kept the old content and the user's removal did not land.
func TestIntegration_DeferredDirectoryCaptureRecordsDeletions(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath := setupIncludeCampaign(t, tc, "capture-deletions")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p payload
		printf 'gone soon\n' > payload/doomed.md
		printf 'stays\n' > payload/kept.md
		git add payload
		git commit -q -m "add payload files"
		rm payload/doomed.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath,
		"workitem", "commit", "--workitem", "capture", "--include", "payload", "-m", "remove doomed")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	drainJobs(t, tc, campPath)

	paths := headPaths(t, tc, campPath)
	assert.NotContains(t, paths, "payload/doomed.md",
		"a deleted tracked file must not survive in the commit; HEAD:\n%s", strings.Join(paths, "\n"))
	assert.Contains(t, paths, "payload/kept.md",
		"the remaining file must still be committed; HEAD:\n%s", strings.Join(paths, "\n"))
}
