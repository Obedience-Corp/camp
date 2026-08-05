//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The delta and whole-file engines differ only in speed. What has to be proven
// is that they differ in nothing else: same files, same manifest, same conflict
// posture. These run the same fixture through both engines over loopback ssh
// and compare the outcomes, plus a persona that proves camp picks the slow one
// on its own when the fast one cannot be trusted.
//
// Every sync here passes --no-probe-cache, and that is load-bearing rather than
// defensive. The engine verdict is cached per machine for 24h, so a run that
// has already probed will not notice a different rsync appearing on PATH — the
// first draft of the parity test failed for exactly that reason, with the
// persona run silently reusing the delta verdict the control run had cached.
// Tests about which engine gets selected have to force the probe, and doing so
// also exercises the flag end to end.

// installOpenRsyncPersona puts a fake rsync earlier on PATH that identifies
// itself as Apple's openrsync but delegates the actual transfer to the real
// binary. That is what makes automatic selection testable: the probe sees an
// untrusted engine while the transfer still moves bytes, so the test observes
// the choice rather than a broken copy.
func installOpenRsyncPersona(t *testing.T, tc *TestContainer) {
	t.Helper()
	tc.Shell(t, `
set -e
mkdir -p /usr/local/bin
cat > /usr/local/bin/rsync <<'PERSONA'
#!/bin/sh
for a in "$@"; do
  if [ "$a" = "--version" ]; then
    echo "openrsync: protocol version 29"
    echo "rsync version 2.6.9 compatible"
    exit 0
  fi
done
exec /usr/bin/rsync "$@"
PERSONA
chmod +x /usr/local/bin/rsync
`)
	// Confirm the persona is what PATH resolves, otherwise the test would
	// silently prove nothing.
	got := tc.Shell(t, `command -v rsync; rsync --version | head -1`)
	require.Contains(t, got, "/usr/local/bin/rsync", "persona must win PATH resolution: %s", got)
	require.Contains(t, got, "openrsync", "persona must identify as openrsync: %s", got)
}

func removeOpenRsyncPersona(t *testing.T, tc *TestContainer) {
	t.Helper()
	tc.Shell(t, "rm -f /usr/local/bin/rsync")
}

// seedArtifactPeer builds a peer campaign holding an artifact root, and a local
// campaign declaring the same root.
func seedArtifactPeer(t *testing.T, tc *TestContainer, name, artifactRoot string) (localRoot, localArtifactRoot string) {
	t.Helper()
	peerRoot := peerCampaignsDir + "/" + name
	localRoot = "/campaigns/" + name
	localArtifactRoot = localRoot + "/" + artifactRoot

	peerSSH(t, tc, fmt.Sprintf(`
set -e
rm -rf %[2]s/%[1]s
camp create %[1]s -d 'engine source' -m 'hold media' --no-git --path %[2]s >/dev/null
mkdir -p %[3]s
printf 'ALPHA-v1' > %[3]s/alpha.bin
printf 'BETA-v1'  > %[3]s/beta.bin
`, name, peerCampaignsDir, shQuote(peerRoot+"/"+artifactRoot)))

	out, err := tc.RunCamp("create", name, "-d", "engine dest", "-m", "pull", "--path", "/campaigns")
	require.NoError(t, err, "local camp create failed: %s", out)
	out, err = tc.RunCampInDir(localRoot, "artifacts", "add", artifactRoot)
	require.NoError(t, err, "artifacts add failed: %s", out)
	return localRoot, localArtifactRoot
}

// syncEngine reads the engine a pull reported from `--json`.
func syncEngine(t *testing.T, out string) (engine, reason string) {
	t.Helper()
	var decoded struct {
		Artifacts []struct {
			Engine       string `json:"engine"`
			EngineReason string `json:"engineReason"`
		} `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &decoded),
		"sync --json was not parseable: %s", out)
	require.NotEmpty(t, decoded.Artifacts, "no artifact results in %s", out)
	return decoded.Artifacts[0].Engine, decoded.Artifacts[0].EngineReason
}

func TestRsyncEngineOverSSH_DeltaIsUsedWhenBothEndsAreModern(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)
	removeOpenRsyncPersona(t, tc)

	localRoot, localArtifactRoot := seedArtifactPeer(t, tc, "enginedelta", "media")

	out, err := tc.RunCampInDir(localRoot, "sync", "--artifacts-only",
		"--from", loopbackMachineID, "--json", "--no-probe-cache")
	require.NoError(t, err, "sync failed: %s", out)

	engine, reason := syncEngine(t, out)
	require.Equal(t, "rsync-delta", engine, "a modern rsync on both ends should use the delta engine: %s", out)
	require.Empty(t, reason, "the delta engine should not carry a fallback reason")

	requireFileContent(t, tc, localArtifactRoot+"/alpha.bin", "ALPHA-v1")
	requireFileContent(t, tc, localArtifactRoot+"/beta.bin", "BETA-v1")
}

func TestRsyncEngineOverSSH_OpenRsyncPersonaSelectsWholeFile(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)
	installOpenRsyncPersona(t, tc)
	defer removeOpenRsyncPersona(t, tc)

	localRoot, localArtifactRoot := seedArtifactPeer(t, tc, "enginewhole", "media")

	out, err := tc.RunCampInDir(localRoot, "sync", "--artifacts-only",
		"--from", loopbackMachineID, "--json", "--no-probe-cache")
	require.NoError(t, err, "sync failed: %s", out)

	engine, reason := syncEngine(t, out)
	require.Equal(t, "whole-file", engine,
		"openrsync on PATH must select the whole-file engine automatically: %s", out)
	require.Contains(t, reason, "openrsync",
		"the report must say why the slow engine ran, not just that it did: %s", out)

	// Degrading is about speed, not correctness: the bytes still arrive.
	requireFileContent(t, tc, localArtifactRoot+"/alpha.bin", "ALPHA-v1")
	requireFileContent(t, tc, localArtifactRoot+"/beta.bin", "BETA-v1")
}

// TestRsyncEngineOverSSH_BothEnginesProduceIdenticalOutcomes is the claim that
// matters: identical from the user's view except speed. The same fixture and
// the same conflict are run through each engine and every user-visible outcome
// is compared — file contents, which file was protected, and the snapshot that
// decides future pulls.
func TestRsyncEngineOverSSH_BothEnginesProduceIdenticalOutcomes(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	type outcome struct {
		engine    string
		alpha     string
		beta      string
		conflicts []string
		snapshot  string
	}

	run := func(t *testing.T, name string, persona bool) outcome {
		t.Helper()
		if persona {
			installOpenRsyncPersona(t, tc)
			defer removeOpenRsyncPersona(t, tc)
		} else {
			removeOpenRsyncPersona(t, tc)
		}

		localRoot, artifactRoot := seedArtifactPeer(t, tc, name, "media")

		// First pull establishes the baseline.
		out, err := tc.RunCampInDir(localRoot, "sync", "--artifacts-only",
			"--from", loopbackMachineID, "--json", "--no-probe-cache")
		require.NoError(t, err, "first sync failed: %s", out)
		engine, _ := syncEngine(t, out)

		// Peer advances both files; the local side edits one of them, which
		// must be protected identically by either engine.
		peerSSH(t, tc, fmt.Sprintf(`
set -e
printf 'ALPHA-PEER-V2'       > %[1]s/alpha.bin
printf 'BETA-PEER-V2-LONGER' > %[1]s/beta.bin
`, shQuote(peerCampaignsDir+"/"+name+"/media")))
		tc.Shell(t, fmt.Sprintf("printf 'ALPHA-LOCAL-EDIT' > %s/alpha.bin", shQuote(artifactRoot)))

		out, err = tc.RunCampInDir(localRoot, "sync", "--artifacts-only",
			"--from", loopbackMachineID, "--json", "--no-probe-cache")
		require.NoError(t, err, "second sync failed: %s", out)

		var decoded struct {
			Artifacts []struct {
				Engine           string   `json:"engine"`
				SkippedConflicts []string `json:"skippedConflicts"`
			} `json:"artifacts"`
		}
		require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &decoded))
		require.NotEmpty(t, decoded.Artifacts)

		snapshot := localRoot + "/.campaign/cache/peersync/" + loopbackMachineID + "/media.json"
		snapContent, err := tc.ReadFile(snapshot)
		require.NoError(t, err, "snapshot missing after sync")

		return outcome{
			engine:    engine,
			alpha:     readFile(t, tc, artifactRoot+"/alpha.bin"),
			beta:      readFile(t, tc, artifactRoot+"/beta.bin"),
			conflicts: decoded.Artifacts[0].SkippedConflicts,
			snapshot:  baselineIdentity(t, snapContent),
		}
	}

	delta := run(t, "engineparitydelta", false)
	whole := run(t, "engineparitywhole", true)

	require.Equal(t, "rsync-delta", delta.engine, "control run must use the delta engine")
	require.Equal(t, "whole-file", whole.engine, "persona run must use the whole-file engine")

	require.Equal(t, delta.alpha, whole.alpha, "the locally edited file must be preserved identically")
	require.Equal(t, "ALPHA-LOCAL-EDIT", whole.alpha, "neither engine may clobber a local edit")
	require.Equal(t, delta.beta, whole.beta, "the unconflicted file must arrive identically")
	require.Equal(t, "BETA-PEER-V2-LONGER", whole.beta)
	require.Equal(t, delta.conflicts, whole.conflicts, "conflict posture must not depend on the engine")
	require.Equal(t, delta.snapshot, whole.snapshot,
		"the agreed baseline (paths, sizes, content hashes) must not depend on the engine")
}

// baselineIdentity reduces a snapshot to the part that must not depend on the
// engine: which files were agreed, how big they are, and their content hashes.
//
// The raw JSON cannot be compared directly because the two runs use separate
// fixtures, so mtimes and generated_at differ by construction — comparing them
// would test the clock. Path plus size plus content hash is both
// fixture-independent and a stronger claim than byte equality of the file:
// it asserts the two engines agreed on the same bytes, not merely on the same
// serialization.
func baselineIdentity(t *testing.T, snapshotJSON string) string {
	t.Helper()
	var manifest struct {
		Root  string `json:"root"`
		Files []struct {
			Path    string `json:"path"`
			Size    int64  `json:"size"`
			Symlink bool   `json:"symlink"`
			Hash    string `json:"hash_sha256"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(snapshotJSON), &manifest),
		"snapshot was not parseable: %s", snapshotJSON)

	lines := make([]string, 0, len(manifest.Files))
	for _, f := range manifest.Files {
		lines = append(lines, fmt.Sprintf("%s size=%d symlink=%v hash=%s",
			f.Path, f.Size, f.Symlink, f.Hash))
	}
	sort.Strings(lines)
	return manifest.Root + "\n" + strings.Join(lines, "\n")
}

// readFile reads a container file, failing the test if it is missing.
func readFile(t *testing.T, tc *TestContainer, path string) string {
	t.Helper()
	content, err := tc.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	return content
}
