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

// `camp project add` is the one bookkeeping commit whose paths name a nested
// repository, and the rest of the project-add coverage runs with deferral off,
// so the deferred form of it had no test at all.
//
// The deferred path captures content by walking the paths it was given. A
// submodule directory walked as ordinary files yields the whole checkout as
// blobs, `projects/<name>/.git` among them, and git refuses that path forever:
// the job cannot succeed on this attempt or any retry.

// The commit must record the submodule pointer, exactly as the synchronous
// path's `git add` would, and must not carry a single file from inside it.
func TestIntegration_ProjectAddDeferredCommitsThePointerNotTheCheckout(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "proj-add-defer")

	source := "/test/proj-add-defer-src"
	name := "proj-add-defer-src"
	require.NoError(t, tc.CreateGitRepo(source))

	// A file below the top level: a walk that descends at all picks this up,
	// so its absence from the commit is what proves the boundary held.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p src
		echo 'package main' > src/main.go
		git add -A
		git commit -q -m 'add source file'
	`, source))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(
		campPath, "project", "add", source, "--local", source)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	drainJobs(t, tc, campPath)

	// The bookkeeping commit camp promised has to exist. A queue that dropped
	// it silently would leave the user's newly added project uncommitted.
	entry := strings.TrimSpace(tc.GitOutput(t, campPath, "ls-tree", "HEAD", "--", "projects/"+name))
	require.NotEmpty(t, entry,
		"the deferred project-add commit never landed; projects/%s is absent from HEAD", name)
	assert.True(t, strings.HasPrefix(entry, "160000 commit"),
		"projects/%s must be committed as a submodule pointer, got %q", name, entry)

	// The failure this test exists for: the checkout committed as ordinary
	// files. `.git` is the entry git rejects outright, but any of them is wrong.
	tree := tc.GitOutput(t, campPath, "ls-tree", "-r", "--name-only", "HEAD")
	for line := range strings.SplitSeq(tree, "\n") {
		path := strings.TrimSpace(line)
		assert.False(t, strings.HasPrefix(path, "projects/"+name+"/"),
			"the commit must not contain files from inside the submodule, found %q", path)
	}

	assert.Contains(t, tc.GitOutput(t, campPath, "ls-tree", "HEAD", "--name-only"), ".gitmodules",
		"the submodule declaration must land in the same commit as the pointer")
}

// A failed job is not self-clearing: it sits in the queue and nags on every
// later command. This one could never drain, because the path git rejected is
// baked into the captured content, so `camp jobs retry` reproduced the same
// failure indefinitely.
func TestIntegration_ProjectAddDeferredLeavesNoUndrainableJob(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "proj-add-defer-queue")

	source := "/test/proj-add-defer-queue-src"
	require.NoError(t, tc.CreateGitRepo(source))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(
		campPath, "project", "add", source, "--local", source)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	drainJobs(t, tc, campPath)

	// The notice is the user-visible symptom: it repeats on unrelated commands
	// until the job is resolved, and here it never could be.
	_, statusErr, _, err := tc.RunCampSplitInDir(campPath, "status", "--short")
	require.NoError(t, err)
	assert.NotContains(t, statusErr, "deferred commit failed",
		"adding a project must not leave a failed job behind; stderr:\n%s", statusErr)

	failed := tc.Shell(t, fmt.Sprintf(
		"ls %s/.campaign/cache/jobs/failed/*/ 2>/dev/null || true", campPath))
	assert.Empty(t, strings.TrimSpace(failed),
		"the failed lane must be empty after a project add drains; found:\n%s", failed)
}

// `camp project new` commits the same two paths as `camp project add`, so it
// reaches the same capture through a second command. Covered separately
// because a fix that only special-cased the add path would leave this one
// producing the same undrainable job.
func TestIntegration_ProjectNewDeferredCommitsThePointerNotTheCheckout(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "proj-new-defer")

	name := "demo-app"
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "project", "new", name)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	drainJobs(t, tc, campPath)

	entry := strings.TrimSpace(tc.GitOutput(t, campPath, "ls-tree", "HEAD", "--", "projects/"+name))
	require.NotEmpty(t, entry,
		"the deferred project-new commit never landed; projects/%s is absent from HEAD", name)
	assert.True(t, strings.HasPrefix(entry, "160000 commit"),
		"projects/%s must be committed as a submodule pointer, got %q", name, entry)

	_, statusErr, _, err := tc.RunCampSplitInDir(campPath, "status", "--short")
	require.NoError(t, err)
	assert.NotContains(t, statusErr, "deferred commit failed",
		"creating a project must not leave a failed job behind; stderr:\n%s", statusErr)
}
