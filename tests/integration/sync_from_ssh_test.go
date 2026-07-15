//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSyncFromPeerOverSSH_FetchesObjectsIntoPeerNamespace drives
// `camp sync --from self --git-only` over a real loopback ssh hop and proves
// the git peer transport: submodule objects that exist only on the peer are
// fetched over ssh (via GIT_SSH_COMMAND, a different channel than the artifact
// rsync's -e) into the local repository's refs/peer/<id>/* namespace, while the
// checkout itself stays on origin's tip. The unit tests build a filesystem
// peer.Source and never dial ssh, so this ssh fetch path is otherwise
// unexercised.
//
// Fixture: a shared submodule origin holds C1. The peer's copy of the submodule
// is advanced to C2 (a child of C1) that is committed on the peer but never
// pushed to origin, so C2 can only reach the local repo through the peer fetch.
func TestSyncFromPeerOverSSH_FetchesObjectsIntoPeerNamespace(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "gitproj"
	peerRoot := peerCampaignsDir + "/" + name
	localRoot := "/campaigns/" + name
	const origin = "/test/sub-origin.git"

	// Shared submodule origin with C1 on main.
	tc.Shell(t, fmt.Sprintf(`
set -e
git init -q --bare %[1]s
rm -rf /test/sub-seed
git init -q /test/sub-seed
cd /test/sub-seed
git config user.email t@t.co && git config user.name T
printf 'v1\n' > f.txt && git add . && git commit -qm C1
git branch -M main && git push -q %[1]s main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main
`, origin))
	c1 := tc.GitOutput(t, origin, "rev-parse", "main")

	// Peer campaign: submodule advanced to C2, committed but not pushed. The
	// resulting sha is written to a shared path root can read back.
	peerSSH(t, tc, fmt.Sprintf(`
set -e
camp create %[1]s -d 'peer git source' -m 'advance sub' --path %[2]s
cd %[3]s
GIT_ALLOW_PROTOCOL=file git submodule add %[4]s projects/sub
git commit -qm 'add sub'
cd projects/sub
printf 'v2\n' > f.txt && git add . && git commit -qm C2
git rev-parse main > /tmp/peer-c2.sha
`, name, peerCampaignsDir, peerRoot, origin))
	c2raw, err := tc.ReadFile("/tmp/peer-c2.sha")
	require.NoError(t, err)
	c2 := strings.TrimSpace(c2raw)
	require.NotEqual(t, c1, c2, "peer submodule tip C2 must differ from origin C1")

	// Local campaign: same submodule at C1 (from origin), initialized.
	createOut, err := tc.RunCamp("create", name, "-d", "local dest", "-m", "sync from peer", "--path", "/campaigns")
	require.NoError(t, err, "local camp create failed: %s", createOut)
	tc.Shell(t, fmt.Sprintf(`
set -e
cd %[1]s
GIT_ALLOW_PROTOCOL=file git submodule add %[2]s projects/sub
git commit -qm 'add sub'
`, localRoot, origin))
	require.Equal(t, c1, tc.GitOutput(t, localRoot+"/projects/sub", "rev-parse", "HEAD"),
		"local submodule should start at origin tip C1")

	// Sync git objects from the peer over ssh (skip artifacts).
	out, err := tc.RunCampInDir(localRoot, "sync", "--from", loopbackMachineID, "--git-only")
	require.NoError(t, err, "git-only peer sync failed: %s", out)

	// C2 arrived over ssh into the peer namespace: objects moved.
	require.Equal(t, c2, tc.GitOutput(t, localRoot+"/projects/sub", "rev-parse", "refs/peer/self/main"),
		"peer commit C2 should be fetched into refs/peer/self/main over ssh")

	// Peer transport is objects-only: the working tree stays on origin's tip
	// (C1), never the peer's un-pushed C2.
	require.Equal(t, c1, tc.GitOutput(t, localRoot+"/projects/sub", "rev-parse", "HEAD"),
		"submodule checkout must stay on origin tip C1, not the peer's un-pushed C2")
}
