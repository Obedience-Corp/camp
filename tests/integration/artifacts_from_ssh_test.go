//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestArtifactsPullOverSSH_TransfersAndProtects drives `camp sync
// --artifacts-only --from self` end to end over a real loopback ssh hop and
// proves the two guarantees the unit tests cannot reach (they build a
// filesystem peer.Source and never dial ssh, so rsync-over-ssh, the ssh
// GIT_SSH_COMMAND wiring, and a genuine source!=dest transfer are all
// unexercised there):
//
//  1. Bytes actually move: declared artifact files present only on the peer
//     arrive in the local root, transferred by rsync over the same ssh options
//     the rest of the peer stack uses.
//  2. No-clobber holds across a divergent second pull: a file the local user
//     edited after the last transfer is protected (kept local), while a file
//     that still matches the recorded baseline is updated to the peer's copy.
//
// The peer is an unprivileged `peer` login user with its own HOME and camp
// registry, so the campaign name resolves to a different root on each end;
// root's local checkout is the pull destination.
func TestArtifactsPullOverSSH_TransfersAndProtects(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "media-camp"
	peerRoot := peerCampaignsDir + "/" + name
	localRoot := "/campaigns/" + name

	// --- Peer side: a same-named campaign holding the source artifacts, in
	// PEER's registry so `camp switch <name> --print` over ssh resolves it.
	peerSSH(t, tc, fmt.Sprintf(`
set -e
camp create %[1]s -d 'artifact source' -m 'hold media' --no-git --path %[2]s
mkdir -p %[3]s/media
printf 'ALPHA-v1' > %[3]s/media/alpha.bin
printf 'BETA-v1'  > %[3]s/media/beta.bin
`, name, peerCampaignsDir, peerRoot))

	// --- Local side: the same-named campaign (root's registry) that declares
	// `media` as an artifact root. Its media dir starts empty, so the first
	// pull is a pure arrival.
	createOut, err := tc.RunCamp("create", name,
		"-d", "artifact dest", "-m", "pull media", "--path", "/campaigns")
	require.NoError(t, err, "local camp create failed: %s", createOut)
	addOut, err := tc.RunCampInDir(localRoot, "artifacts", "add", "media")
	require.NoError(t, err, "artifacts add failed: %s", addOut)

	// --- Pull #1: first sync. Both files are new locally, so both transfer;
	// there is no baseline yet, so nothing is protected.
	out1, err := tc.RunCampInDir(localRoot, "sync", "--artifacts-only", "--from", loopbackMachineID)
	require.NoError(t, err, "first artifact sync failed: %s", out1)
	require.Contains(t, out1, "first sync", "first pull should report first-sync semantics: %s", out1)

	requireFileContent(t, tc, localRoot+"/media/alpha.bin", "ALPHA-v1")
	requireFileContent(t, tc, localRoot+"/media/beta.bin", "BETA-v1")

	// A baseline snapshot for this peer+root must be recorded so the next pull
	// can tell a local edit from a stale copy. Root "media" slugs to "media".
	snapshot := localRoot + "/.campaign/cache/peersync/" + loopbackMachineID + "/media.json"
	exists, err := tc.CheckFileExists(snapshot)
	require.NoError(t, err)
	require.True(t, exists, "peer snapshot %s should exist after first sync", snapshot)

	// --- Diverge: the peer advances both files; locally the user edits alpha
	// (so it no longer matches the baseline) but leaves beta at the synced
	// bytes. Sizes are distinct so conflict detection is unambiguous even where
	// the filesystem's mtime granularity is coarse.
	peerSSH(t, tc, fmt.Sprintf(`
set -e
printf 'ALPHA-PEER-V2'       > %[1]s/media/alpha.bin
printf 'BETA-PEER-V2-LONGER' > %[1]s/media/beta.bin
`, peerRoot))
	require.NoError(t, tc.WriteFile(localRoot+"/media/alpha.bin", "ALPHA-LOCAL-EDIT"))

	// --- Pull #2: no-clobber under a real transfer. alpha differs from the
	// baseline (local edit) and must be kept; beta still matches the baseline
	// and must be updated to the peer's copy.
	out2, err := tc.RunCampInDir(localRoot, "sync", "--artifacts-only", "--from", loopbackMachineID)
	require.NoError(t, err, "second artifact sync failed: %s", out2)
	require.Contains(t, out2, "conflicts kept local", "second pull should report the kept conflict: %s", out2)
	// Conflicts are reported relative to the artifact root ("alpha.bin", not
	// "media/alpha.bin").
	require.Contains(t, out2, "alpha.bin (local edit preserved", "the conflicting file should be named: %s", out2)

	requireFileContent(t, tc, localRoot+"/media/alpha.bin", "ALPHA-LOCAL-EDIT")
	requireFileContent(t, tc, localRoot+"/media/beta.bin", "BETA-PEER-V2-LONGER")
}

// requireFileContent asserts the container file at path holds exactly want.
func requireFileContent(t *testing.T, tc *TestContainer, path, want string) {
	t.Helper()
	got, err := tc.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	require.Equal(t, want, strings.TrimRight(got, "\n"), "content of %s", path)
}
