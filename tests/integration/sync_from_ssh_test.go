//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSyncFromPeerOverSSH_FetchesObjectsIntoPeerNamespace verifies SSH peer
// object transfer without changing the checkout.
func TestSyncFromPeerOverSSH_FetchesObjectsIntoPeerNamespace(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "gitproj"
	peerRoot := peerCampaignsDir + "/" + name
	localRoot := "/campaigns/" + name
	const origin = "/test/sub-origin.git"

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
