//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// The bundle path is what a cold seed falls back to when the peer is being
// written to. These cases make the peer genuinely non-quiescent and then assert
// the outcome is still a correct clone — which is the only thing that matters
// to a user, and the reason the fallback is automatic rather than a flag.

func TestBundleSeedOverSSH_NonQuiescentRootSeedsViaBundle(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "bundleroot"
	peerRoot, rootOrigin := seedColdSeedPeer(t, tc, name)

	// Uncommitted content makes the root non-quiescent, so the copy path must
	// decline and the bundle must carry the seed.
	peerSSH(t, tc, fmt.Sprintf("printf 'in-flight\\n' > %s/scratch.txt", peerRoot))

	localRoot := "/campaigns/" + name
	out, err := tc.RunCampInDir("/campaigns", "clone", rootOrigin, localRoot,
		"--from", loopbackMachineID, "--no-submodules", "--no-validate", "--json")
	require.NoError(t, err, "bundle seed failed: %s", out)

	// Assert the transport actually used, not the progress line: that line is
	// printed before the bundle runs, so it stays true even if the bundle fails
	// and the seed silently degrades to a slower path.
	require.Equal(t, "bundle", seedMethodFor(t, out, "."),
		"a non-quiescent root must be delivered by the bundle transport")

	requireFileContent(t, tc, localRoot+"/marker.txt", "root-marker\n")
	require.Equal(t, rootOrigin, tc.GitOutput(t, localRoot, "remote", "get-url", "origin"),
		"origin must be the canonical url, never the scratch bundle file")
	require.Equal(t,
		trimLine(peerSSH(t, tc, "cd "+peerRoot+" && git rev-parse HEAD")),
		tc.GitOutput(t, localRoot, "rev-parse", "HEAD"),
		"bundle-seeded root must sit on the peer's committed HEAD")

	// The peer's uncommitted file is not committed, so it must not cross.
	exists, err := tc.CheckFileExists(localRoot + "/scratch.txt")
	require.NoError(t, err)
	require.False(t, exists, "a bundle carries commits, never the peer's working tree")
}

func TestBundleSeedOverSSH_LeavesNoScratchBehind(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "bundlescratch"
	peerRoot, rootOrigin := seedColdSeedPeer(t, tc, name)
	peerSSH(t, tc, fmt.Sprintf("printf 'in-flight\\n' > %s/scratch.txt", peerRoot))

	localRoot := "/campaigns/" + name
	out, err := tc.RunCampInDir("/campaigns", "clone", rootOrigin, localRoot,
		"--from", loopbackMachineID, "--no-submodules", "--no-validate")
	require.NoError(t, err, "bundle seed failed: %s", out)

	// The bundle is a scratch file next to the destination; a seed that leaves
	// one behind leaves a repo-sized file in the user's campaigns directory.
	leftovers, _, execErr := tc.ExecCommand("sh", "-c",
		"ls -d /campaigns/.camp-bundle-* 2>/dev/null | wc -l")
	require.NoError(t, execErr)
	require.Equal(t, "0", trimLine(leftovers), "bundle scratch dirs must be cleaned up")
}

func TestBundleSeedOverSSH_NonQuiescentSubmoduleStillArrives(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "bundlesub"
	peerRoot, rootOrigin := seedColdSeedPeer(t, tc, name)

	// Dirty only the submodule: the root stays quiescent, so this also proves
	// the two paths run side by side in one clone — copy for the root, bundle
	// for the submodule — which is what per-repo granularity buys.
	peerSSH(t, tc, fmt.Sprintf("printf 'sub-dirty\\n' > %s/projects/sub/scratch.txt", peerRoot))

	localRoot := "/campaigns/" + name
	out, err := tc.RunCampInDir("/campaigns", "clone", rootOrigin, localRoot,
		"--from", loopbackMachineID, "--json")
	require.NoError(t, err, "clone with a dirty submodule failed: %s", out)

	require.Equal(t, "pack-copy", seedMethodFor(t, out, "."),
		"the quiescent root should still take the fast copy path")
	require.Equal(t, "bundle", seedMethodFor(t, out, "projects/sub"),
		"the dirty submodule must be delivered by the bundle transport")

	requireFileContent(t, tc, localRoot+"/projects/sub/f.txt", "sub-v1\n")
	require.Equal(t,
		trimLine(peerSSH(t, tc, "cd "+peerRoot+"/projects/sub && git rev-parse HEAD")),
		tc.GitOutput(t, localRoot+"/projects/sub", "rev-parse", "HEAD"),
		"the submodule must arrive at the peer's recorded commit via the bundle")

	exists, err := tc.CheckFileExists(localRoot + "/projects/sub/scratch.txt")
	require.NoError(t, err)
	require.False(t, exists, "the peer's uncommitted submodule file must not cross")

	// The submodule's origin must be the declared URL, not the scratch bundle.
	require.NotContains(t, tc.GitOutput(t, localRoot+"/projects/sub", "remote", "get-url", "origin"),
		".camp-bundle-", "submodule origin must be re-pointed off the scratch bundle")
}

func TestBundleSeedOverSSH_UnreachablePeerStillClonesFromOrigin(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "bundleunreach"
	_, rootOrigin := seedColdSeedPeer(t, tc, name)

	// Point the machine at a host that does not answer. Every peer route fails,
	// and the clone must still succeed from origin: a peer is an optimisation,
	// never a dependency.
	require.NoError(t, tc.WriteFile("/root/.obey/machines.yaml", fmt.Sprintf(`version: 1
machines:
  - id: %s
    label: unreachable
    host: 203.0.113.1
    auth_method: ssh-agent
    ssh_user: nobody
    identity_file: %s
`, loopbackMachineID, rootIdentity)))
	defer registerLoopbackMachine(t, tc)

	localRoot := "/campaigns/" + name
	out, err := tc.RunCampInDir("/campaigns", "clone", rootOrigin, localRoot,
		"--from", loopbackMachineID, "--no-submodules", "--no-validate")
	require.NoError(t, err, "an unreachable peer must degrade to origin, not fail: %s", out)

	requireFileContent(t, tc, localRoot+"/marker.txt", "root-marker\n")
	require.Equal(t, rootOrigin, tc.GitOutput(t, localRoot, "remote", "get-url", "origin"))
}

// seedMethodFor reads the transport a clone reported for one repository from
// its --json seed summary.
func seedMethodFor(t *testing.T, out, repo string) string {
	t.Helper()
	var decoded struct {
		Seed *struct {
			Repos []struct {
				Repo   string `json:"repo"`
				Method string `json:"method"`
			} `json:"repos"`
		} `json:"seed"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &decoded))
	require.NotNil(t, decoded.Seed, "clone reported no seed summary: %s", out)
	for _, r := range decoded.Seed.Repos {
		if r.Repo == repo {
			return r.Method
		}
	}
	t.Fatalf("no seed entry for %q in %+v", repo, decoded.Seed.Repos)
	return ""
}
