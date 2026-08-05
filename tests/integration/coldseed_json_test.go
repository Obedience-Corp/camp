//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// --from is an addition to clone, not a change to it. These cases hold both
// halves of that: the seed summary is present and accurate when a peer is used,
// and the JSON a scripted caller already parses is untouched when one is not.

func TestCloneJSON_NoPeerEmitsNoSeedKey(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "jsongolden"
	_, rootOrigin := seedColdSeedPeer(t, tc, name)

	localRoot := "/campaigns/" + name
	out, err := tc.RunCampInDir("/campaigns", "clone", rootOrigin, localRoot,
		"--no-submodules", "--no-validate", "--json")
	require.NoError(t, err, "origin clone failed: %s", out)

	raw := jsonPayload(t, out)
	require.NotContains(t, raw, "\"seed\"",
		"a clone without --from must emit byte-identical JSON, with no seed key")

	// And it is still valid, complete output rather than merely seed-free.
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded))
	require.Equal(t, true, decoded["success"])
	require.Contains(t, decoded, "directory")
}

func TestCloneJSON_PeerSeedReportsPerRepoMethod(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	registerLoopbackMachine(t, tc)

	const name = "jsonseed"
	peerRoot, rootOrigin := seedColdSeedPeer(t, tc, name)

	// Root quiescent, submodule dirty: the two transports must be reported
	// distinctly, which is the whole reason the summary is per repo.
	peerSSH(t, tc, fmt.Sprintf("printf 'dirty\\n' > %s/projects/sub/scratch.txt", peerRoot))

	localRoot := "/campaigns/" + name
	out, err := tc.RunCampInDir("/campaigns", "clone", rootOrigin, localRoot,
		"--from", loopbackMachineID, "--json")
	require.NoError(t, err, "peer clone failed: %s", out)

	var decoded struct {
		Success bool `json:"success"`
		Seed    *struct {
			Repos []struct {
				Repo   string `json:"repo"`
				Method string `json:"method"`
				Reason string `json:"reason"`
			} `json:"repos"`
		} `json:"seed"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonPayload(t, out)), &decoded))
	require.NotNil(t, decoded.Seed, "a clone with --from must report a seed summary: %s", out)

	methods := map[string]string{}
	reasons := map[string]string{}
	for _, r := range decoded.Seed.Repos {
		methods[r.Repo] = r.Method
		reasons[r.Repo] = r.Reason
	}
	require.Equal(t, "pack-copy", methods["."],
		"a quiescent root should report the copy transport: %+v", decoded.Seed.Repos)
	require.Equal(t, "bundle", methods["projects/sub"],
		"a dirty submodule should report the bundle transport: %+v", decoded.Seed.Repos)
	require.Contains(t, reasons["projects/sub"], "not quiescent",
		"a fallback must say why it fell back")
	require.Empty(t, reasons["."], "the fast path must not invent a fallback reason")
}

// jsonPayload extracts the JSON document from command output, which may carry
// human-readable progress lines ahead of it.
func jsonPayload(t *testing.T, out string) string {
	t.Helper()
	idx := strings.Index(out, "{")
	require.GreaterOrEqual(t, idx, 0, "no JSON document in output: %s", out)
	return out[idx:]
}
