//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusStderr returns what camp status writes to stderr, where notices go.
func statusStderr(t *testing.T, tc *TestContainer, campPath string) string {
	t.Helper()
	_, stderr, _, err := tc.RunCampSplitInDir(campPath, "status")
	require.NoError(t, err)
	return stderr
}

// Criterion 31b: a declared root that has never synced is the notice that
// justifies this surface — declaring moved the bytes out of git's care, and
// until a sync runs there is one copy of them anywhere.
func TestIntegration_StatusNoticeNeverSynced(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "notice-never-synced")

	tc.Shell(t, fmt.Sprintf(`cd %s && mkdir -p media/renders && printf 'x' > media/renders/a.bin`, campPath))
	declareRoots(t, tc, campPath, "media/renders")

	stderr := statusStderr(t, tc, campPath)
	assert.Contains(t, stderr, "media/renders")
	assert.Contains(t, stderr, "never synced")
	assert.Contains(t, stderr, "camp sync --from")
	assert.Contains(t, stderr, "camp notify dismiss",
		"every dismissible notice must carry its own dismiss command")

	// A recorded snapshot means it has left this machine; the notice stops.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p .campaign/cache/peersync/laptop
		printf '{"version":1,"root":"media/renders","files":[]}' > .campaign/cache/peersync/laptop/media%%2Frenders.json
	`, campPath))

	stderr = statusStderr(t, tc, campPath)
	assert.NotContains(t, stderr, "never synced",
		"the notice must stop after the first successful sync")
}

// Criterion 31c: a declared root absent locally is one line naming the count.
func TestIntegration_StatusNoticeMissingLocally(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "notice-missing-local")
	declareRoots(t, tc, campPath, "not/here", "also/absent")

	stderr := statusStderr(t, tc, campPath)
	assert.Contains(t, stderr, "2 declared artifact roots are not on this machine")
	assert.Contains(t, stderr, "camp sync --from")
}

// Criterion 31d: zero declared roots means zero work and no change to output.
func TestIntegration_StatusNoticeSilentWithoutRoots(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "notice-no-roots")

	stderr := statusStderr(t, tc, campPath)
	assert.NotContains(t, stderr, "artifact root")
	assert.NotContains(t, stderr, "never synced")
	assert.NotContains(t, stderr, "not on this machine")
}

// Criterion 31f: dismissal is per signature. Silencing one root must not
// silence a root declared later.
func TestIntegration_StatusNoticeDismissalIsPerSignature(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "notice-dismiss")

	tc.Shell(t, fmt.Sprintf(`cd %s && mkdir -p first && printf 'x' > first/a.bin`, campPath))
	declareRoots(t, tc, campPath, "first")

	stderr := statusStderr(t, tc, campPath)
	require.Contains(t, stderr, "first")

	out, err := tc.RunCampInDir(campPath, "notify", "dismiss", "artifact-root-never-synced:first")
	require.NoError(t, err, "output:\n%s", out)
	assert.Contains(t, out, "Dismissed")
	assert.Contains(t, out, "Undo: camp notify restore")

	stderr = statusStderr(t, tc, campPath)
	assert.NotContains(t, stderr, "first is a declared artifact root",
		"the dismissed root must go quiet")

	// A newly declared root has its own signature and notifies anyway.
	tc.Shell(t, fmt.Sprintf(`cd %s && mkdir -p second && printf 'x' > second/b.bin`, campPath))
	declareRoots(t, tc, campPath, "first", "second")

	stderr = statusStderr(t, tc, campPath)
	assert.Contains(t, stderr, "second",
		"a newly declared root must notify despite an older dismissal")
}

// The dismissal is committed, so it travels between machines the same way the
// declarations it concerns do.
func TestIntegration_StatusNoticeDismissalIsCommitted(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "notice-dismiss-committed")
	declareRoots(t, tc, campPath, "somewhere")

	_, err := tc.RunCampInDir(campPath, "notify", "dismiss", "artifact-roots-missing-locally")
	require.NoError(t, err)

	content, err := tc.ReadFile(campPath + "/.campaign/notices.yaml")
	require.NoError(t, err, "dismissals must live in a committed file, not cache")
	assert.Contains(t, content, "artifact-roots-missing-locally")

	// It is inside .campaign/, which is committed, and not under cache/.
	exists, err := tc.CheckFileExists(campPath + "/.campaign/cache/notices.yaml")
	require.NoError(t, err)
	assert.False(t, exists, "dismissals must not live in machine-local cache")

	out, err := tc.RunCampInDir(campPath, "notify", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "artifact-roots-missing-locally")

	// Restore brings it back.
	_, err = tc.RunCampInDir(campPath, "notify", "restore", "artifact-roots-missing-locally")
	require.NoError(t, err)
	stderr := statusStderr(t, tc, campPath)
	assert.Contains(t, stderr, "not on this machine")
}

// Criterion 31e: the backward-looking case is doctor's, never status's. A
// deliberate gitignore is not a defect to nag about on every command.
func TestIntegration_StatusNoticeNeverMentionsUnownedFiles(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupBigFilesCampaign(t, tc, "notice-no-bigfiles")

	stderr := statusStderr(t, tc, campPath)
	assert.NotContains(t, stderr, "media/raw/footage.mov")
	assert.NotContains(t, stderr, "owned by no system")

	// doctor still reports it, so the state is discoverable, just not here.
	found := bigFilesFindings(t, tc, campPath)
	assert.Contains(t, found, "media/raw/footage.mov",
		"the state must remain discoverable through doctor")
}
