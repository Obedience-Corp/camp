//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// The cold-seed path byte-copies a quiescent peer's object store instead of
// making the peer's git build a pack. What has to be proven is not that bytes
// moved but that the result is indistinguishable from an origin clone: same
// content, same commit, origin pointed at the real URL, and a working tree git
// itself checked out. These drive the real `camp clone --from` binary, so the
// whole pipeline runs — quiescence check, copy, HEAD re-verify, connectivity,
// git completion, validation.

// seedColdSeedPeer builds a peer campaign with one submodule and pushes the
// root to a bare origin, returning the peer root and the origin URL.
func seedColdSeedPeer(t *testing.T, tc *TestContainer, name string) (peerRoot, rootOrigin string) {
	t.Helper()
	peerRoot = peerCampaignsDir + "/" + name
	// The root origin lives under /tmp and is created by the peer, because the
	// peer has to push to it and a root-owned bare repo is not peer-writable.
	// The submodule origin is only ever read, so root can own it.
	rootOrigin = "/tmp/" + name + "-root.git"
	subOrigin := "/test/" + name + "-sub.git"

	// A bare repo's HEAD must name the branch that was actually pushed; git
	// defaults it to master, and cloning a repo whose HEAD names a missing
	// branch fails with "branch yet to be born".
	tc.Shell(t, fmt.Sprintf(`
set -e
rm -rf %[1]s /test/%[2]s-seed
git init -q --bare %[1]s
git init -q /test/%[2]s-seed
cd /test/%[2]s-seed
git config user.email t@t.co && git config user.name T
printf 'sub-v1\n' > f.txt
git add . && git commit -qm C1
git branch -M main && git push -q %[1]s main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main
chmod -R a+rX %[1]s
`, subOrigin, name))

	peerSSH(t, tc, fmt.Sprintf(`
set -e
rm -rf %[2]s %[4]s
camp create %[1]s -d 'cold seed source' -m 'seed' --path %[3]s >/dev/null
cd %[2]s
printf 'root-marker\n' > marker.txt
GIT_ALLOW_PROTOCOL=file git -c protocol.file.allow=always submodule add -q %[5]s projects/sub
git add -A
git commit -qm 'add sub and scaffold'
git init -q --bare %[4]s
git remote add origin %[4]s
branch="$(git rev-parse --abbrev-ref HEAD)"
git push -q origin "$branch"
git --git-dir %[4]s symbolic-ref HEAD "refs/heads/$branch"
chmod -R a+rX %[4]s
`, name, peerRoot, peerCampaignsDir, rootOrigin, subOrigin))

	return peerRoot, rootOrigin
}

// trimLine strips the trailing newline a shell command leaves on a value.
func trimLine(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

func TestColdSeedOverSSH_QuiescentPeerSeedsAndMatchesOriginClone(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "coldseedok"
	peerRoot, rootOrigin := seedColdSeedPeer(t, tc, name)

	localRoot := "/campaigns/" + name
	out, err := tc.RunCampInDir("/campaigns", "clone", rootOrigin, localRoot, "--from", loopbackMachineID)
	require.NoError(t, err, "cold-seed clone failed: %s", out)

	// Content arrived.
	requireFileContent(t, tc, localRoot+"/marker.txt", "root-marker\n")

	// Origin is the real URL, not the peer: the seed is a transport detail and
	// must not survive into the clone's configuration.
	require.Equal(t, rootOrigin, tc.GitOutput(t, localRoot, "remote", "get-url", "origin"),
		"origin must be re-pointed to the canonical url after a cold seed")

	// The commit matches the peer's, and the object store is intact — a torn
	// copy would fail this walk.
	require.Equal(t,
		trimLine(peerSSH(t, tc, "cd "+peerRoot+" && git rev-parse HEAD")),
		tc.GitOutput(t, localRoot, "rev-parse", "HEAD"),
		"cold-seeded root must sit on the same commit as the peer")
	_, exit, execErr := tc.ExecCommand("sh", "-c",
		"git -C "+localRoot+" rev-list --objects --all >/dev/null")
	require.NoError(t, execErr)
	require.Equal(t, 0, exit, "every object reachable from the copied refs must be present")

	// git checked the working tree out and it matches the commit: a copy that
	// arrived with missing or extra content would show up here.
	require.Empty(t, tc.GitOutput(t, localRoot, "status", "--porcelain=v1", "--ignore-submodules=dirty"),
		"a cold-seeded clone must have a clean working tree")
}

func TestColdSeedOverSSH_SeedsSubmoduleWithoutCloning(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "coldseedsub"
	peerRoot, rootOrigin := seedColdSeedPeer(t, tc, name)

	localRoot := "/campaigns/" + name
	out, err := tc.RunCampInDir("/campaigns", "clone", rootOrigin, localRoot, "--from", loopbackMachineID)
	require.NoError(t, err, "cold-seed clone failed: %s", out)

	// The submodule's content is present and at the recorded commit.
	requireFileContent(t, tc, localRoot+"/projects/sub/f.txt", "sub-v1\n")
	require.Equal(t,
		trimLine(peerSSH(t, tc, "cd "+peerRoot+"/projects/sub && git rev-parse HEAD")),
		tc.GitOutput(t, localRoot+"/projects/sub", "rev-parse", "HEAD"),
		"cold-seeded submodule must sit on the peer's recorded commit")

	// And its objects are complete on their own.
	_, exit, execErr := tc.ExecCommand("sh", "-c",
		"git -C "+localRoot+"/projects/sub rev-list --objects --all >/dev/null")
	require.NoError(t, execErr)
	require.Equal(t, 0, exit, "submodule object store must be connectivity-clean")
}

func TestColdSeedOverSSH_DirtyPeerStillProducesAValidClone(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "coldseeddirty"
	_, rootOrigin := seedColdSeedPeer(t, tc, name)

	// Make the peer's campaign root non-quiescent. The cold-seed copy must
	// decline and the clone must still succeed by another route — degrading is
	// the contract, not failing.
	peerSSH(t, tc, fmt.Sprintf("printf 'uncommitted\\n' > %s/dirty.txt", peerCampaignsDir+"/"+name))

	localRoot := "/campaigns/" + name
	out, err := tc.RunCampInDir("/campaigns", "clone", rootOrigin, localRoot, "--from", loopbackMachineID)
	require.NoError(t, err, "clone from a non-quiescent peer must still succeed: %s", out)

	requireFileContent(t, tc, localRoot+"/marker.txt", "root-marker\n")
	require.Equal(t, rootOrigin, tc.GitOutput(t, localRoot, "remote", "get-url", "origin"),
		"origin must be the canonical url regardless of which peer route ran")

	// The peer's uncommitted file is working-tree state and must never ride
	// along: only committed content crosses.
	exists, err := tc.CheckFileExists(localRoot + "/dirty.txt")
	require.NoError(t, err)
	require.False(t, exists, "a peer's uncommitted file must never arrive in the clone")
}

func TestColdSeedOverSSH_MatchesAPlainOriginClone(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "coldseedparity"
	_, rootOrigin := seedColdSeedPeer(t, tc, name)

	// `--from` resolves the campaign on the peer by the target directory's
	// name, so both clones keep the campaign name and differ by parent dir.
	seeded := "/campaigns/seeded/" + name
	plain := "/campaigns/plain/" + name

	// Root repo only. The fixture's submodule origin is a local path and git
	// refuses file transport for submodules (CVE-2022-39253), so a plain
	// origin clone cannot initialise it. --no-submodules must not fail
	// validation for skipping that init. Full validation is exercised by
	// the quiescent-peer case above.
	flags := []string{"--no-submodules"}
	out, err := tc.RunCampInDir("/campaigns", append([]string{"clone", rootOrigin, seeded,
		"--from", loopbackMachineID}, flags...)...)
	require.NoError(t, err, "cold-seed clone failed: %s", out)
	out, err = tc.RunCampInDir("/campaigns", append([]string{"clone", rootOrigin, plain}, flags...)...)
	require.NoError(t, err, "origin clone failed: %s", out)

	// The whole point of finishing in git: the two routes are the same repo.
	require.Equal(t,
		tc.GitOutput(t, plain, "rev-parse", "HEAD"),
		tc.GitOutput(t, seeded, "rev-parse", "HEAD"),
		"a peer-seeded clone and an origin clone must land on the same commit")
	require.Equal(t,
		tc.GitOutput(t, plain, "rev-parse", "HEAD^{tree}"),
		tc.GitOutput(t, seeded, "rev-parse", "HEAD^{tree}"),
		"a peer-seeded clone and an origin clone must have identical trees")
	require.Equal(t,
		tc.GitOutput(t, plain, "remote", "get-url", "origin"),
		tc.GitOutput(t, seeded, "remote", "get-url", "origin"),
		"both routes must end pointed at the same origin")
	require.Equal(t,
		tc.GitOutput(t, plain, "rev-parse", "--abbrev-ref", "HEAD"),
		tc.GitOutput(t, seeded, "rev-parse", "--abbrev-ref", "HEAD"),
		"both routes must end on the same branch, not a detached head")
}
