//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCloneFromPeerOverSSH_SeedsAndRepointsOrigin verifies peer-seeded cloning.
func TestCloneFromPeerOverSSH_SeedsAndRepointsOrigin(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "cloneproj"
	peerRoot := peerCampaignsDir + "/" + name
	const campOrigin = "/tmp/camp-origin.git"

	peerSSH(t, tc, fmt.Sprintf(`
set -e
rm -rf %[4]s
camp create %[1]s -d 'peer clone source' -m 'seed from peer' --path %[2]s
cd %[3]s
printf 'peer-seeded\n' > marker.txt
git add marker.txt && git commit -qm 'add marker'
git init -q --bare %[4]s
git remote add origin %[4]s
git push -q origin "$(git rev-parse --abbrev-ref HEAD)"
`, name, peerCampaignsDir, peerRoot, campOrigin))

	localRoot := "/campaigns/" + name
	out, err := tc.RunCampInDir("/campaigns", "clone", campOrigin, localRoot,
		"--from", loopbackMachineID, "--no-submodules")
	require.NoError(t, err, "clone --from peer failed: %s", out)

	requireFileContent(t, tc, localRoot+"/marker.txt", "peer-seeded\n")

	require.Equal(t, campOrigin, tc.GitOutput(t, localRoot, "remote", "get-url", "origin"),
		"origin should be re-pointed to the canonical url after seeding from the peer")
}
