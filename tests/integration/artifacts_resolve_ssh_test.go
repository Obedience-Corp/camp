//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Resolve exists because the no-clobber protection is sticky: once a local
// file diverges from the last agreed state, every later sync keeps refusing to
// touch it, and before this command the only escape was deleting the file.
// These drive the real lifecycle — conflict, resolve, sync again — because the
// claim being made is about what the NEXT sync does, which a unit test cannot
// observe.

// seedConflict builds a peer + local campaign sharing an artifact root, syncs
// once to establish a baseline, then creates a real conflict on alpha.bin.
func seedConflict(t *testing.T, tc *TestContainer, name string) (localRoot, artifactRoot, peerArtifact string) {
	t.Helper()
	peerRoot := peerCampaignsDir + "/" + name
	localRoot = "/campaigns/" + name
	artifactRoot = localRoot + "/media"
	peerArtifact = shQuote(peerRoot + "/media")

	peerSSH(t, tc, fmt.Sprintf(`
set -e
rm -rf %[2]s/%[1]s
camp create %[1]s -d 'conflict source' -m 'hold media' --no-git --path %[2]s >/dev/null
mkdir -p %[3]s
printf 'ALPHA-v1' > %[3]s/alpha.bin
printf 'BETA-v1'  > %[3]s/beta.bin
`, name, peerCampaignsDir, peerArtifact))

	out, err := tc.RunCamp("create", name, "-d", "conflict dest", "-m", "pull", "--path", "/campaigns")
	require.NoError(t, err, "local create failed: %s", out)
	out, err = tc.RunCampInDir(localRoot, "artifacts", "add", "media")
	require.NoError(t, err, "artifacts add failed: %s", out)

	// Establish the agreed baseline.
	out, err = tc.RunCampInDir(localRoot, "sync", "--artifacts-only", "--from", loopbackMachineID)
	require.NoError(t, err, "baseline sync failed: %s", out)
	requireFileContent(t, tc, artifactRoot+"/alpha.bin", "ALPHA-v1")

	// Both sides move: this is a genuine conflict, not a stale copy.
	peerSSH(t, tc, fmt.Sprintf("printf 'ALPHA-PEER-v2' > %s/alpha.bin", peerArtifact))
	tc.Shell(t, fmt.Sprintf("printf 'ALPHA-LOCAL-EDIT' > %s/alpha.bin", shQuote(artifactRoot)))

	out, err = tc.RunCampInDir(localRoot, "sync", "--artifacts-only", "--from", loopbackMachineID)
	require.NoError(t, err, "conflicting sync failed: %s", out)
	requireFileContent(t, tc, artifactRoot+"/alpha.bin", "ALPHA-LOCAL-EDIT")
	return localRoot, artifactRoot, peerArtifact
}

// resolveList returns the conflict paths camp reports for the loopback peer.
func resolveList(t *testing.T, tc *TestContainer, localRoot string) []string {
	t.Helper()
	out, err := tc.RunCampInDir(localRoot, "artifacts", "resolve",
		"--list", "--from", loopbackMachineID, "--json")
	require.NoError(t, err, "resolve --list failed: %s", out)

	var decoded struct {
		Conflicts []struct {
			Root string `json:"root"`
			Path string `json:"path"`
		} `json:"conflicts"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &decoded))
	paths := make([]string, 0, len(decoded.Conflicts))
	for _, c := range decoded.Conflicts {
		paths = append(paths, c.Path)
	}
	return paths
}

func TestArtifactsResolveOverSSH_ListReportsTheStuckConflict(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	localRoot, _, _ := seedConflict(t, tc, "resolvelist")

	require.Equal(t, []string{"alpha.bin"}, resolveList(t, tc, localRoot),
		"the conflicted file, and only it, must be listed")

	// Listing is the dry-run: it must not touch anything.
	require.Equal(t, []string{"alpha.bin"}, resolveList(t, tc, localRoot),
		"--list must be idempotent and non-mutating")
}

func TestArtifactsResolveOverSSH_TakeLocalKeepsBytesAcrossTheNextSync(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	localRoot, artifactRoot, _ := seedConflict(t, tc, "resolvelocal")

	out, err := tc.RunCampInDir(localRoot, "artifacts", "resolve", "alpha.bin",
		"--from", loopbackMachineID, "--take-local")
	require.NoError(t, err, "resolve --take-local failed: %s", out)

	require.Empty(t, resolveList(t, tc, localRoot), "the conflict must be cleared from the report")
	requireFileContent(t, tc, artifactRoot+"/alpha.bin", "ALPHA-LOCAL-EDIT")

	// This is the assertion that matters, and the reason take-local removes the
	// baseline entry rather than writing the local one into it: recording the
	// local entry as agreed is exactly the state a pull treats as safe to
	// overwrite, so a naive implementation loses the user's file on the very
	// next sync — one command after they chose to keep it.
	out, err = tc.RunCampInDir(localRoot, "sync", "--artifacts-only", "--from", loopbackMachineID)
	require.NoError(t, err, "post-resolve sync failed: %s", out)
	requireFileContent(t, tc, artifactRoot+"/alpha.bin", "ALPHA-LOCAL-EDIT")

	// And it stays resolved rather than re-reporting the same conflict.
	require.Empty(t, resolveList(t, tc, localRoot), "the conflict must not come back after a sync")

	// The unconflicted file still syncs normally: pinning one path must not
	// freeze the root.
	requireFileContent(t, tc, artifactRoot+"/beta.bin", "BETA-v1")
}

func TestArtifactsResolveOverSSH_TakePeerReplacesBytesAndClears(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	localRoot, artifactRoot, _ := seedConflict(t, tc, "resolvepeer")

	out, err := tc.RunCampInDir(localRoot, "artifacts", "resolve", "alpha.bin",
		"--from", loopbackMachineID, "--take-peer", "--json")
	require.NoError(t, err, "resolve --take-peer failed: %s", out)

	var result struct {
		Action      string `json:"action"`
		Path        string `json:"path"`
		NewBaseline *struct {
			Size int64 `json:"size"`
		} `json:"newBaseline"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &result))
	require.Equal(t, "take-peer", result.Action)
	require.Equal(t, "alpha.bin", result.Path)
	require.NotNil(t, result.NewBaseline, "take-peer must report the new agreed entry: %s", out)

	// The peer's bytes are in place, and the conflict is genuinely gone rather
	// than muted.
	requireFileContent(t, tc, artifactRoot+"/alpha.bin", "ALPHA-PEER-v2")
	require.Empty(t, resolveList(t, tc, localRoot), "the conflict must be cleared")

	// A following sync is a no-op for that file: local, baseline and peer all
	// agree now.
	out, err = tc.RunCampInDir(localRoot, "sync", "--artifacts-only", "--from", loopbackMachineID)
	require.NoError(t, err, "post-resolve sync failed: %s", out)
	requireFileContent(t, tc, artifactRoot+"/alpha.bin", "ALPHA-PEER-v2")
	require.Empty(t, resolveList(t, tc, localRoot))
}

func TestArtifactsResolveOverSSH_UnknownPathErrorsPrecisely(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	localRoot, _, _ := seedConflict(t, tc, "resolveunknown")

	// A path with no conflict must say so and name what IS conflicted, rather
	// than failing with a bare "not found" the user cannot act on.
	out, err := tc.RunCampInDir(localRoot, "artifacts", "resolve", "beta.bin",
		"--from", loopbackMachineID, "--take-local")
	require.Error(t, err, "resolving a clean path must fail")
	require.Contains(t, out, "not an open conflict")
	require.Contains(t, out, "alpha.bin", "the error should name the conflicts that ARE open: %s", out)

	// Choosing no side is a usage error, not a silent default.
	out, err = tc.RunCampInDir(localRoot, "artifacts", "resolve", "alpha.bin",
		"--from", loopbackMachineID)
	require.Error(t, err, "resolve without a side must fail")
	require.Contains(t, out, "choose a side")

	// Both sides at once is contradictory.
	out, err = tc.RunCampInDir(localRoot, "artifacts", "resolve", "alpha.bin",
		"--from", loopbackMachineID, "--take-local", "--take-peer")
	require.Error(t, err, "contradictory flags must fail")
	require.Contains(t, out, "mutually exclusive")
}
