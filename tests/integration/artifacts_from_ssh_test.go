//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestArtifactsPullOverSSH_TransfersAndProtects verifies SSH artifact transfer,
// including a spaced root and no-clobber behavior.
func TestArtifactsPullOverSSH_TransfersAndProtects(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const (
		name         = "media-camp"
		artifactRoot = "Final Renders"
	)
	peerRoot := peerCampaignsDir + "/" + name
	localRoot := "/campaigns/" + name
	peerArtifactRoot := shQuote(peerRoot + "/" + artifactRoot)
	localArtifactRoot := localRoot + "/" + artifactRoot

	peerSSH(t, tc, fmt.Sprintf(`
set -e
camp create %[1]s -d 'artifact source' -m 'hold media' --no-git --path %[2]s
mkdir -p %[3]s
printf 'ALPHA-v1' > %[3]s/alpha.bin
printf 'BETA-v1'  > %[3]s/beta.bin
`, name, peerCampaignsDir, peerArtifactRoot))

	createOut, err := tc.RunCamp("create", name,
		"-d", "artifact dest", "-m", "pull media", "--path", "/campaigns")
	require.NoError(t, err, "local camp create failed: %s", createOut)
	addOut, err := tc.RunCampInDir(localRoot, "artifacts", "add", artifactRoot)
	require.NoError(t, err, "artifacts add failed: %s", addOut)

	out1, err := tc.RunCampInDir(localRoot, "sync", "--artifacts-only", "--from", loopbackMachineID)
	require.NoError(t, err, "first artifact sync failed: %s", out1)
	require.Contains(t, out1, "first sync", "first pull should report first-sync semantics: %s", out1)

	requireFileContent(t, tc, localArtifactRoot+"/alpha.bin", "ALPHA-v1")
	requireFileContent(t, tc, localArtifactRoot+"/beta.bin", "BETA-v1")

	snapshot := localRoot + "/.campaign/cache/peersync/" + loopbackMachineID + "/" + artifactRoot + ".json"
	exists, err := tc.CheckFileExists(snapshot)
	require.NoError(t, err)
	require.True(t, exists, "peer snapshot %s should exist after first sync", snapshot)

	peerSSH(t, tc, fmt.Sprintf(`
set -e
printf 'ALPHA-PEER-V2'       > %[1]s/alpha.bin
printf 'BETA-PEER-V2-LONGER' > %[1]s/beta.bin
`, peerArtifactRoot))
	tc.Shell(t, fmt.Sprintf("printf 'ALPHA-LOCAL-EDIT' > %s/alpha.bin", shQuote(localArtifactRoot)))

	out2, err := tc.RunCampInDir(localRoot, "sync", "--artifacts-only", "--from", loopbackMachineID)
	require.NoError(t, err, "second artifact sync failed: %s", out2)
	require.Contains(t, out2, "1 conflict kept local", "second pull should report the kept conflict: %s", out2)
	require.Contains(t, out2, "alpha.bin (local edit preserved", "the conflicting file should be named: %s", out2)

	requireFileContent(t, tc, localArtifactRoot+"/alpha.bin", "ALPHA-LOCAL-EDIT")
	requireFileContent(t, tc, localArtifactRoot+"/beta.bin", "BETA-PEER-V2-LONGER")
}

func requireFileContent(t *testing.T, tc *TestContainer, path, want string) {
	t.Helper()
	got, err := tc.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	require.Equal(t, want, got, "content of %s", path)
}

// The machine that holds a root's bytes can still pull from a peer. When the
// peer's root is empty or absent, nothing came across and the bytes have still
// never left this machine, so the never-synced notice must survive the pull.
func TestArtifactsPullFromEmptyOrAbsentPeerRootKeepsNeverSyncedNotice(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const artifactRoot = "renders"
	cases := []struct {
		name         string
		peerHasRoot  bool
		wantSnapshot bool
	}{
		{name: "never-synced-empty-peer", peerHasRoot: true, wantSnapshot: true},
		{name: "never-synced-absent-peer", peerHasRoot: false, wantSnapshot: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			peerRoot := peerCampaignsDir + "/" + c.name
			script := fmt.Sprintf("set -e\ncamp create %s -d 'peer' -m 'empty peer' --no-git --path %s\n",
				c.name, peerCampaignsDir)
			if c.peerHasRoot {
				script += fmt.Sprintf("mkdir -p %s\n", shQuote(peerRoot+"/"+artifactRoot))
			}
			peerSSH(t, tc, script)

			localRoot := "/campaigns/" + c.name
			createOut, err := tc.RunCamp("create", c.name, "-d", "holds media", "-m", "source", "--path", "/campaigns")
			require.NoError(t, err, "local camp create failed: %s", createOut)
			tc.Shell(t, fmt.Sprintf("mkdir -p %[1]s/%[2]s && printf 'only-here' > %[1]s/%[2]s/a.bin", localRoot, artifactRoot))
			// This machine's own record is the baseline the notice measures
			// coverage against, so the real manifest job writes it first.
			declareAndRecord(t, tc, localRoot, artifactRoot)

			syncOut, _ := tc.RunCampInDir(localRoot, "sync", "--artifacts-only", "--from", loopbackMachineID)
			t.Logf("sync output:\n%s", syncOut)

			snapshot := localRoot + "/.campaign/cache/peersync/" + loopbackMachineID + "/" + artifactRoot + ".json"
			exists, err := tc.CheckFileExists(snapshot)
			require.NoError(t, err)
			require.Equal(t, c.wantSnapshot, exists, "snapshot presence after the pull")
			if exists {
				content, err := tc.ReadFile(snapshot)
				require.NoError(t, err)
				t.Logf("snapshot: %s", content)
				require.NotContains(t, content, "a.bin", "a file the peer never had must not be recorded as agreed")
			}

			requireFileContent(t, tc, localRoot+"/"+artifactRoot+"/a.bin", "only-here")
			require.Contains(t, campAs(t, tc, noticeMachine, localRoot, "status"), "never synced",
				"nothing left this machine, so the notice must stay")
		})
	}
}
