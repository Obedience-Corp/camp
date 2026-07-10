//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// installStubTailscale writes a fake `tailscale` binary to PATH that answers
// `status --json` with a fixed fixture (Self "Mac Studio" + one Peer
// "Devbox"), mirroring cmd/camp/testdata/tailscale_status.json. Real tailscale
// is never installed or invoked by these tests.
func installStubTailscale(t *testing.T, tc *TestContainer) {
	t.Helper()
	script := `#!/bin/sh
if [ "$1" = "status" ] && [ "$2" = "--json" ]; then
cat <<'JSON'
{
  "BackendState": "Running",
  "Self": {
    "HostName": "Mac Studio",
    "DNSName": "mac-studio.tail37114b.ts.net.",
    "TailscaleIPs": ["100.64.0.5"],
    "Online": true,
    "OS": "macOS"
  },
  "Peer": {
    "nodekey:1": {
      "HostName": "Devbox",
      "DNSName": "devbox.tail37114b.ts.net.",
      "TailscaleIPs": ["100.64.0.1"],
      "Online": true,
      "OS": "linux"
    }
  }
}
JSON
  exit 0
fi
exit 1
`
	require.NoError(t, tc.WriteFile("/usr/local/bin/tailscale", script))
	_, _, err := tc.ExecCommand("chmod", "+x", "/usr/local/bin/tailscale")
	require.NoError(t, err)
	// The pooled container's Reset() does not clean /usr/local/bin, so remove the
	// stub after this test or a later test asserting "no tailscale" would find it.
	t.Cleanup(func() { _, _, _ = tc.ExecCommand("rm", "-f", "/usr/local/bin/tailscale") })
}

// TestMachineAddDiscoverYes proves --discover --yes runs fully non-interactively
// (no TTY in the container) by taking the first (sorted) discovered device and
// writing it through machines.Save with auth_method=tailscale-ssh.
func TestMachineAddDiscoverYes(t *testing.T) {
	tc := GetSharedContainer(t)
	installStubTailscale(t, tc)

	out, err := tc.RunCamp("machine", "add", "--discover", "--yes")
	require.NoError(t, err, "machine add --discover --yes failed: %s", out)

	payload := machineListJSON(t, tc)
	row, ok := findMachine(payload.Machines, "devbox")
	require.True(t, ok, "devbox missing from list --json: %+v", payload.Machines)
	require.Equal(t, "devbox.tail37114b.ts.net", row.Host)
	require.Equal(t, "tailscale-ssh", row.AuthMethod)
	require.Empty(t, row.SSHUser, "tailscale-ssh needs no ssh_user")
	require.Empty(t, row.IdentityFile, "tailscale-ssh needs no identity_file")
}

// TestMachineAddDiscoverByID proves the positional-id selection path (picking a
// discovered device by its derived id without the interactive picker) and that
// re-running --discover against the same device updates in place rather than
// duplicating — the same Upsert idempotency manual `add` gets.
func TestMachineAddDiscoverByID(t *testing.T) {
	tc := GetSharedContainer(t)
	installStubTailscale(t, tc)

	out, err := tc.RunCamp("machine", "add", "--discover", "devbox")
	require.NoError(t, err, "machine add --discover devbox failed: %s", out)

	payload := machineListJSON(t, tc)
	row, ok := findMachine(payload.Machines, "devbox")
	require.True(t, ok, "devbox missing from list --json: %+v", payload.Machines)
	require.Equal(t, "tailscale-ssh", row.AuthMethod)

	out, err = tc.RunCamp("machine", "add", "--discover", "devbox")
	require.NoError(t, err, "second machine add --discover devbox failed: %s", out)
	payload = machineListJSON(t, tc)
	count := 0
	for _, m := range payload.Machines {
		if m.ID == "devbox" {
			count++
		}
	}
	require.Equal(t, 1, count, "expected exactly one devbox row after repeat discover: %+v", payload.Machines)
}

// TestMachineAddDiscoverMissingBinary proves a container with no tailscale on
// PATH gets a clear, actionable error rather than a raw exec failure or hang.
func TestMachineAddDiscoverMissingBinary(t *testing.T) {
	tc := GetSharedContainer(t)
	// Defensively remove any tailscale stub a prior discover test may have left in
	// the pooled container (Reset does not clean /usr/local/bin), so the precondition
	// "no tailscale on PATH" holds regardless of test order.
	_, _, _ = tc.ExecCommand("rm", "-f", "/usr/local/bin/tailscale")

	out, err := tc.RunCamp("machine", "add", "--discover", "--yes")
	require.Error(t, err, "machine add --discover with no tailscale on PATH should fail: %s", out)
	require.Contains(t, out, "tailscale", "error should mention tailscale: %s", out)
}
