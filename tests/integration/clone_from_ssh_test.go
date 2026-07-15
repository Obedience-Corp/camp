//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCloneFromPeerOverSSH_SeedsAndRepointsOrigin drives `camp clone <url>
// <dir> --from self` over a real loopback ssh hop and proves the peer-seeded
// clone path: the campaign is cloned from the peer's copy over ssh (fast path),
// then origin is re-pointed to the canonical url and the delta fetched — so the
// result is an origin replica that arrived over the peer. The unit tests build
// a filesystem peer.Source and never dial ssh, so this ssh clone path is
// otherwise unexercised.
//
// The peer owns a registered campaign it has pushed to a bare origin under /tmp
// (peer-writable). marker.txt is content unique to the peer's checkout, present
// only because the seed came from the peer.
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

	// Clone: seed from the peer over ssh, then re-point origin to campOrigin.
	localRoot := "/campaigns/" + name
	out, err := tc.RunCampInDir("/campaigns", "clone", campOrigin, localRoot,
		"--from", loopbackMachineID, "--no-submodules")
	require.NoError(t, err, "clone --from peer failed: %s", out)

	// The peer-seeded content arrived.
	requireFileContent(t, tc, localRoot+"/marker.txt", "peer-seeded")

	// Origin was re-pointed to the canonical url, not left as the peer path.
	require.Equal(t, campOrigin, tc.GitOutput(t, localRoot, "remote", "get-url", "origin"),
		"origin should be re-pointed to the canonical url after seeding from the peer")
}
