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
	require.Contains(t, out2, "conflicts kept local", "second pull should report the kept conflict: %s", out2)
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
