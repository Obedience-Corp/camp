//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/clone"
	"github.com/stretchr/testify/require"
)

// The quiescence contract (D001) is what makes a byte copy of a peer's pack
// files safe, so it has to be proven against real repositories rather than
// against fixture strings. These cases run the production script
// (clone.QuiescenceScript) on the peer over the loopback ssh the #428 harness
// established, through the same `sh -lc` wrapping peer.Source.RunShell uses,
// and hand the bytes to the production parser (clone.ParseQuiescenceReport).
// Script and parser are therefore exercised exactly as shipped; the ssh
// invocation itself is remote.Run, already covered by the other _from_ssh
// cases in this package.

// collectQuiescence runs the real script on the peer and parses the real
// output into a report.
func collectQuiescence(t *testing.T, tc *TestContainer, peerRoot string) *clone.QuiescenceReport {
	t.Helper()
	out := peerSSH(t, tc, clone.QuiescenceScript(peerRoot))
	report, err := clone.ParseQuiescenceReport(loopbackMachineID, peerRoot, []byte(out))
	require.NoError(t, err, "parsing peer quiescence output failed; raw output:\n%s", out)
	return report
}

// requireVerdict asserts one repository's quiescence, reporting the peer's own
// reasons when the expectation misses so a failure explains itself.
func requireVerdict(t *testing.T, r *clone.QuiescenceReport, repo string, wantQuiescent bool) clone.RepoVerdict {
	t.Helper()
	v, ok := r.Verdict(repo)
	require.True(t, ok, "no verdict reported for %q; report: %+v", repo, r.Repos)
	require.Equal(t, wantQuiescent, v.Quiescent,
		"repo %q quiescent=%v, want %v (reasons: %v)", repo, v.Quiescent, wantQuiescent, v.Reasons)
	return v
}

// seedQuiescenceCampaign creates a peer campaign with one submodule and
// returns the campaign root on the peer.
func seedQuiescenceCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()
	peerRoot := peerCampaignsDir + "/" + name
	subOrigin := "/test/" + name + "-sub.git"

	tc.Shell(t, fmt.Sprintf(`
set -e
rm -rf %[1]s /test/%[2]s-seed
git init -q --bare %[1]s
git init -q /test/%[2]s-seed
cd /test/%[2]s-seed
git config user.email t@t.co && git config user.name T
printf 'v1\n' > f.txt && git add . && git commit -qm C1
git branch -M main && git push -q %[1]s main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main
chmod -R a+rX %[1]s
`, subOrigin, name))

	// The scaffold is committed because a settled campaign is what the contract
	// is about: camp create leaves its scaffolding untracked, and untracked
	// files are non-quiescent under D001.
	peerSSH(t, tc, fmt.Sprintf(`
set -e
rm -rf %[2]s
camp create %[1]s -d 'quiescence fixture' -m 'seed' --path %[3]s
cd %[2]s
GIT_ALLOW_PROTOCOL=file git submodule add %[4]s projects/sub
git add -A
git commit -qm 'add sub and commit scaffold'
`, name, peerRoot, peerCampaignsDir, subOrigin))

	return peerRoot
}

func TestQuiescenceOverSSH_QuiescentCampaignPassesEveryRepo(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	peerRoot := seedQuiescenceCampaign(t, tc, "quiescecleanproj")
	report := collectQuiescence(t, tc, peerRoot)

	require.True(t, report.Quiescent(),
		"a settled campaign must be quiescent; non-quiescent: %+v", report.NonQuiescent())
	require.Equal(t, loopbackMachineID, report.MachineID)
	require.Equal(t, peerRoot, report.Root)

	root := requireVerdict(t, report, ".", true)
	sub := requireVerdict(t, report, "projects/sub", true)

	// The recorded HEADs are what the post-copy re-verify compares against, so
	// they must be the peer's real shas, and the submodule must report its own.
	require.Equal(t, tc.GitOutput(t, peerRoot, "rev-parse", "HEAD"), root.HeadSHA)
	require.Equal(t, tc.GitOutput(t, peerRoot+"/projects/sub", "rev-parse", "HEAD"), sub.HeadSHA)
	require.NotEqual(t, root.HeadSHA, sub.HeadSHA,
		"root and submodule must report distinct HEADs, not one repo answering twice")
}

func TestQuiescenceOverSSH_DirtySubmoduleFailsOnlyThatRepo(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	peerRoot := seedQuiescenceCampaign(t, tc, "quiescedirtyproj")
	peerSSH(t, tc, fmt.Sprintf(`
set -e
printf 'uncommitted\n' > %s/projects/sub/f.txt
`, peerRoot))

	report := collectQuiescence(t, tc, peerRoot)

	require.False(t, report.Quiescent(), "a campaign with a dirty submodule is not quiescent")
	requireVerdict(t, report, ".", true)
	sub := requireVerdict(t, report, "projects/sub", false)
	require.Contains(t, strings.Join(sub.Reasons, "; "), "uncommitted changes")

	// Per-repo granularity is the point of D001: the fallback stays surgical.
	failed := report.NonQuiescent()
	require.Len(t, failed, 1, "only the dirty submodule should fail: %+v", failed)
	require.Equal(t, "projects/sub", failed[0].Repo)

	// And the verdicts are actionable: the per-repo pack-copy / bundle split
	// sequence 02 implements is a direct read of this report, with no second
	// source of truth. Asserted here rather than in its own case because it
	// needs exactly this peer state.
	got := map[string]string{}
	for _, v := range report.Repos {
		got[v.Repo] = selectTransfer(v)
	}
	require.Equal(t, map[string]string{
		".":            "pack-copy",
		"projects/sub": "bundle",
	}, got, "verdicts must drive a per-repo transfer choice, not an all-or-nothing one")
}

func TestQuiescenceOverSSH_StaleIndexLockFailsThatRepo(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	peerRoot := seedQuiescenceCampaign(t, tc, "quiescelockproj")

	// A submodule's git dir is .git/modules/<name>, not <sub>/.git, so this
	// also proves the script resolves the real git dir rather than guessing.
	peerSSH(t, tc, fmt.Sprintf(`
set -e
gitdir=$(git -C %s/projects/sub rev-parse --absolute-git-dir)
: > "$gitdir/index.lock"
`, peerRoot))

	report := collectQuiescence(t, tc, peerRoot)

	require.False(t, report.Quiescent(), "a stale index.lock must block the byte copy")
	requireVerdict(t, report, ".", true)
	sub := requireVerdict(t, report, "projects/sub", false)
	require.Contains(t, strings.Join(sub.Reasons, "; "), "index.lock")

	// The working tree is clean, so the lock alone is what disqualified it, and
	// HEAD is still readable and recorded.
	require.NotEmpty(t, sub.HeadSHA, "a locked-but-readable repo should still report HEAD")
	require.NotContains(t, strings.Join(sub.Reasons, "; "), "uncommitted changes")
}

// D006: a concurrent fetch holds a lock under refs/ while writing new objects
// into the pack directory a cold seed copies, and the git dir's own *.lock glob
// does not see it. The lock is created directly here because racing a real
// fetch is not deterministic, and it is the lock's presence the contract acts on.
func TestQuiescenceOverSSH_RefLockFailsThatRepo(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	peerRoot := seedQuiescenceCampaign(t, tc, "quiescereflock")
	peerSSH(t, tc, fmt.Sprintf(`
set -e
gitdir=$(git -C %s/projects/sub rev-parse --absolute-git-dir)
mkdir -p "$gitdir/refs/heads"
: > "$gitdir/refs/heads/main.lock"
`, peerRoot))

	report := collectQuiescence(t, tc, peerRoot)

	require.False(t, report.Quiescent(), "a ref lock must block the byte copy (D006)")
	requireVerdict(t, report, ".", true)
	sub := requireVerdict(t, report, "projects/sub", false)
	require.Contains(t, strings.Join(sub.Reasons, "; "), "main.lock",
		"the verdict must name the ref lock so a stale lock is distinguishable from a live fetch: %v", sub.Reasons)

	// The working tree is clean, so the ref lock alone disqualified it.
	require.NotContains(t, strings.Join(sub.Reasons, "; "), "uncommitted changes")
}

func TestQuiescenceOverSSH_MidOperationRepoFails(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	peerRoot := seedQuiescenceCampaign(t, tc, "quiescemidopproj")

	// Leave the submodule genuinely mid-merge: a conflicted merge stops with
	// MERGE_HEAD written, which is the "in-flight git operation" case.
	peerSSH(t, tc, fmt.Sprintf(`
cd %s/projects/sub
git config user.email t@t.co && git config user.name T
git checkout -q -b side
printf 'side\n' > f.txt && git add . && git commit -qm side
git checkout -q main
printf 'main\n' > f.txt && git add . && git commit -qm main
git merge side >/dev/null 2>&1
exit 0
`, peerRoot))

	report := collectQuiescence(t, tc, peerRoot)

	sub := requireVerdict(t, report, "projects/sub", false)
	require.Contains(t, strings.Join(sub.Reasons, "; "), "git operation in progress",
		"a repo stopped mid-merge must report the in-flight operation: %v", sub.Reasons)
	require.Contains(t, strings.Join(sub.Reasons, "; "), "MERGE_HEAD")
}

func TestQuiescenceOverSSH_ConcurrentCommitFlipsTheReVerify(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	peerRoot := seedQuiescenceCampaign(t, tc, "quiesceraceproj")

	before := collectQuiescence(t, tc, peerRoot)
	require.True(t, before.Quiescent(), "peer starts settled: %+v", before.NonQuiescent())
	rootBefore, _ := before.Verdict(".")
	subBefore, _ := before.Verdict("projects/sub")

	// The peer commits between the verdict and the re-check, then settles. A
	// commit writes new objects into the very pack directory a cold seed byte
	// copies, so the copy can straddle it — but nothing is dirty afterwards, so
	// a re-check that only re-ran the clean-status check would call this
	// quiescent and ship the straddled copy. The recorded HEAD is what turns
	// the race into a detected condition.
	peerSSH(t, tc, fmt.Sprintf(`
set -e
cd %s
printf 'raced\n' > raced.txt
git add raced.txt && git commit -qm 'peer moved under us'
`, peerRoot))

	after := collectQuiescence(t, tc, peerRoot)

	require.True(t, after.Quiescent(),
		"the peer is settled again, so status alone cannot detect the race: %+v", after.NonQuiescent())
	rootAfter := requireVerdict(t, after, ".", true)
	require.NotEqual(t, rootBefore.HeadSHA, rootAfter.HeadSHA,
		"HEAD must move, otherwise this case proves nothing")

	// This is the post-copy re-verify sequence 02 performs.
	require.False(t, headsUnchanged(before, after),
		"re-verify must reject a peer whose HEAD moved during the copy window")

	// Re-verify is per-repo: the untouched submodule is still trustworthy, so a
	// detected race costs one repo's fallback, not the whole seed.
	subAfter := requireVerdict(t, after, "projects/sub", true)
	require.Equal(t, subBefore.HeadSHA, subAfter.HeadSHA,
		"an untouched submodule must survive another repo's race")
}

func TestQuiescenceOverSSH_UninitializedSubmoduleNeverInheritsRootVerdict(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	peerRoot := seedQuiescenceCampaign(t, tc, "quiesceabsentproj")

	// Deinit leaves an empty directory inside the campaign repository. git
	// rev-parse from inside one walks UP and answers for the campaign root, so
	// without a guard the absent submodule inherits the root's clean status and
	// the root's HEAD — a false quiescent verdict for content that is not there.
	peerSSH(t, tc, fmt.Sprintf(`
set -e
cd %s
git submodule deinit -f projects/sub >/dev/null 2>&1
mkdir -p projects/sub
`, peerRoot))

	report := collectQuiescence(t, tc, peerRoot)

	rootVerdict := requireVerdict(t, report, ".", true)
	sub := requireVerdict(t, report, "projects/sub", false)
	require.Empty(t, sub.HeadSHA,
		"an absent submodule must report no HEAD, never the campaign root's")
	require.NotEqual(t, rootVerdict.HeadSHA, sub.HeadSHA)
	require.Contains(t, strings.Join(sub.Reasons, "; "), "not a git repository")
}

// selectTransfer is the stub of the transfer step's decision: quiescent repos
// are byte-copied, everything else falls back to a bundle.
func selectTransfer(v clone.RepoVerdict) string {
	if v.Quiescent {
		return "pack-copy"
	}
	return "bundle"
}

// headsUnchanged reports whether every repo quiescent in before still reports
// the same HEAD in after — the post-copy re-verify.
func headsUnchanged(before, after *clone.QuiescenceReport) bool {
	for _, b := range before.Repos {
		if !b.Quiescent {
			continue
		}
		a, ok := after.Verdict(b.Repo)
		if !ok || a.HeadSHA != b.HeadSHA {
			return false
		}
	}
	return true
}
