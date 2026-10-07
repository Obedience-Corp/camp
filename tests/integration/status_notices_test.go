//go:build integration
// +build integration

package integration

import (
	"fmt"
	"regexp"
	"strings"
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

// noticeMachine pins this machine's manifest identity, so the record the
// manifest job writes and the record the detector reads are the same file
// whatever the container's hostname is.
const noticeMachine = "this-box"

// declareAndRecord declares root and commits, letting the real manifest job
// write this machine's record of it: the baseline coverage is measured against.
func declareAndRecord(t *testing.T, tc *TestContainer, campPath, root string) string {
	t.Helper()
	campAs(t, tc, noticeMachine, campPath, "artifacts add "+root)
	campAs(t, tc, noticeMachine, campPath, `commit -m "record `+root+`"`)
	settleJobs(t, tc, noticeMachine, campPath)
	own := fmt.Sprintf("%s/.campaign/artifacts/manifests/%s/%s.json",
		campPath, noticeMachine, strings.ReplaceAll(root, "/", "%2F"))
	exists, err := tc.CheckFileExists(own)
	require.NoError(t, err)
	require.True(t, exists, "the manifest job must have written this machine's record at %s", own)
	return own
}

// Criterion 31b: a declared root that has never synced is the notice that
// justifies this surface — declaring moved the bytes out of git's care, and
// until a sync runs there is one copy of them anywhere.
func TestIntegration_StatusNoticeNeverSynced(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "notice-never-synced")

	tc.Shell(t, fmt.Sprintf(`cd %s && mkdir -p media/renders && printf 'x' > media/renders/a.bin`, campPath))
	own := declareAndRecord(t, tc, campPath, "media/renders")

	out := campAs(t, tc, noticeMachine, campPath, "status")
	assert.Contains(t, out, "media/renders")
	assert.Contains(t, out, "never synced")
	assert.Contains(t, out, "on another machine: camp sync --from <id of "+noticeMachine+">",
		"the remedy runs on the machine that does not hold the bytes")
	assert.Contains(t, out, "--artifacts-only")
	assert.Regexp(t, `camp notify dismiss never-synced-[0-9a-f]{6}\)`, out,
		"every dismissible notice must carry its own short dismiss id")
	assert.NotContains(t, out, "artifact-root-never-synced:",
		"the root path must not be part of the id")

	// Pulling from a peer whose root is empty records a snapshot that agreed
	// on nothing. The bytes have still never left this machine.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p .campaign/cache/peersync/laptop
		printf '{"version":1,"root":"media/renders","files":[]}' > .campaign/cache/peersync/laptop/media%%2Frenders.json
	`, campPath))
	out = campAs(t, tc, noticeMachine, campPath, "status")
	assert.Contains(t, out, "never synced", "an empty snapshot is not a second copy")

	// Snapshots carry no hash, so the mtime has to match this machine's record.
	// One a nanosecond off describes other bytes.
	snapshot := campPath + "/.campaign/cache/peersync/laptop/media%2Frenders.json"
	tc.Shell(t, fmt.Sprintf(`jq '{version: 1, root: .root, files: [.files[] | del(.hash_sha256) | .mtime_unix_nano -= 1]}' '%s' > '%s'`,
		own, snapshot))
	out = campAs(t, tc, noticeMachine, campPath, "status")
	assert.Contains(t, out, "never synced", "a snapshot with another mtime is not a copy of these bytes")

	// A snapshot that agreed on this machine's file means it has left.
	tc.Shell(t, fmt.Sprintf(`jq '{version: 1, root: .root, files: [.files[] | del(.hash_sha256)]}' '%s' > '%s'`,
		own, snapshot))
	out = campAs(t, tc, noticeMachine, campPath, "status")
	assert.NotContains(t, out, "never synced",
		"the notice must stop once every recorded file has a second copy")
}

// The pull that makes a second copy runs on the other machine, so the snapshot
// it writes never reaches this one. Its committed manifest does, through git,
// and the notice clears only when that manifest holds every file this
// machine's record holds.
func TestIntegration_StatusNoticeNeverSyncedClearsFromAnotherMachinesManifest(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "notice-never-synced-manifest")

	tc.Shell(t, fmt.Sprintf(`cd %s && mkdir -p media/renders && printf 'x' > media/renders/a.bin`, campPath))
	own := declareAndRecord(t, tc, campPath, "media/renders")
	otherDir := campPath + "/.campaign/artifacts/manifests/other-studio"
	other := otherDir + "/media%2Frenders.json"

	out := campAs(t, tc, noticeMachine, campPath, "status")
	assert.Contains(t, out, "never synced", "this machine's own record is not a second copy")

	// A machine without the root commits an empty manifest for it.
	tc.Shell(t, fmt.Sprintf(`mkdir -p %s && printf '{"version":1,"root":"media/renders","describes_commit":"x","files":[]}' > '%s'`,
		otherDir, other))
	out = campAs(t, tc, noticeMachine, campPath, "status")
	assert.Contains(t, out, "never synced", "an empty manifest from another machine is not a second copy")

	// The other machine pulled and its record matches this one's.
	tc.Shell(t, fmt.Sprintf(`cp '%s' '%s'`, own, other))
	out = campAs(t, tc, noticeMachine, campPath, "status")
	assert.NotContains(t, out, "never synced", "another machine holding every file is a second copy")
	assert.NotContains(t, out, "on this machine only")

	// A file added here since then exists here only, and the notice counts it.
	tc.Shell(t, fmt.Sprintf(`cd %s && printf 'yy' > media/renders/b.bin`, campPath))
	campAs(t, tc, noticeMachine, campPath, `commit -m "add b"`)
	settleJobs(t, tc, noticeMachine, campPath)
	out = campAs(t, tc, noticeMachine, campPath, "status")
	assert.Contains(t, out, "1 of 2 files under media/renders exists on this machine only",
		"a partial copy must name how many files have none")
}

// camp v0.10.0 committed dismissals under ids carrying the whole root path.
// They keep working after the id change, and every notify verb accepts them.
func TestIntegration_StatusNoticeV010DismissalsMigrate(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "notice-legacy-dismissal")

	tc.Shell(t, fmt.Sprintf(`cd %s && mkdir -p media/renders && printf 'x' > media/renders/a.bin`, campPath))
	declareRoots(t, tc, campPath, "media/renders")
	shortID := neverSyncedID(t, statusStderr(t, tc, campPath))

	require.NoError(t, tc.WriteFile(campPath+"/.campaign/notices.yaml",
		"version: 1\ndismissed:\n    artifact-root-never-synced:media/renders: 2026-09-01T00:00:00Z\n"))
	stderr := statusStderr(t, tc, campPath)
	assert.NotContains(t, stderr, "never synced", "a v0.10.0 dismissal must still silence its notice")

	out, err := tc.RunCampInDir(campPath, "notify", "list")
	require.NoError(t, err, "output:\n%s", out)
	assert.Contains(t, out, shortID, "list shows the current id")
	assert.Contains(t, out, "media/renders", "list names what the id is about")
	assert.Contains(t, out, "never synced to another machine")
	assert.NotContains(t, out, "artifact-root-never-synced:")

	out, err = tc.RunCampInDir(campPath, "notify", "restore", "artifact-root-never-synced:media/renders")
	require.NoError(t, err, "output:\n%s", out)
	assert.Contains(t, out, "Restored "+shortID)
	assert.Contains(t, statusStderr(t, tc, campPath), "never synced")

	out, err = tc.RunCampInDir(campPath, "notify", "dismiss", "artifact-root-never-synced:media/renders")
	require.NoError(t, err, "output:\n%s", out)
	assert.Contains(t, out, "Dismissed "+shortID)
	content, err := tc.ReadFile(campPath + "/.campaign/notices.yaml")
	require.NoError(t, err)
	assert.Contains(t, content, shortID)
	assert.NotContains(t, content, "artifact-root-never-synced:", "a save writes the current form")
}

var neverSyncedIDPattern = regexp.MustCompile(`never-synced-[0-9a-f]{6}`)

// neverSyncedID returns the dismiss id a never-synced notice printed.
func neverSyncedID(t *testing.T, stderr string) string {
	t.Helper()
	id := neverSyncedIDPattern.FindString(stderr)
	require.NotEmpty(t, id, "no never-synced id in:\n%s", stderr)
	return id
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

	out, err := tc.RunCampInDir(campPath, "notify", "dismiss", neverSyncedID(t, stderr))
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
