//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRemoteSwitchLocalhostSshSmoke proves, end to end, that
// `camp switch <machine>:<campaign> --shell-connect` resolves a remote
// campaign root over a real ssh hop and that the resolve step
// (internal/remote.ResolveRoot) and the interactive hop share a single
// ControlMaster connection.
//
// The container plays both "local" and "remote": the registered machine
// ("self") points ssh at localhost, so root == remote root by construction.
// This is a loopback smoke test of the ssh plumbing in
// internal/remote/ssh.go and cmd/camp/switch.go's runRemoteSwitch, not a
// test of true cross-host behavior.
func TestRemoteSwitchLocalhostSshSmoke(t *testing.T) {
	tc := GetSharedContainer(t)

	// Provision sshd + root loopback key (idempotent; shared with the peer
	// transport smoke tests). This test drives the ssh hop as root, so the
	// `self` machine it registers below uses ssh_user=root and resolves the
	// one campaign identically on both ends.
	ensureSSHDaemon(t, tc)

	// --- Create a campaign that both the "local" and "remote" camp switch
	// resolve identically, since remote == local == this container here.
	const base = "/campaigns"
	const campaignName = "remote-smoke-campaign"
	createOut, err := tc.RunCamp("create", campaignName,
		"-d", "remote switch ssh smoke test",
		"-m", "prove the loopback ssh hop",
		"--no-git",
		"--path", base,
	)
	require.NoError(t, err, "camp create should succeed; output: %s", createOut)

	printOut, err := tc.RunCamp("switch", campaignName, "--print")
	require.NoError(t, err, "camp switch --print should resolve the local campaign; output: %s", printOut)
	root := strings.TrimSpace(printOut)
	require.NotEmpty(t, root, "resolved campaign root must not be empty")

	// --- Register the loopback machine in ~/.obey/machines.yaml.
	machinesYAML := `version: 1
machines:
  - id: self
    label: Self (loopback smoke test)
    host: localhost
    auth_method: ssh-agent
    ssh_user: root
    identity_file: /root/.ssh/id_ed25519
`
	require.NoError(t, tc.WriteFile("/root/.obey/machines.yaml", machinesYAML))

	// --- The feature under test: resolve the remote root over ssh and emit
	// the shell-connect line.
	shellConnectOut, err := tc.RunCamp("switch", "self:"+campaignName, "--shell-connect")
	require.NoError(t, err, "camp switch self:%s --shell-connect should succeed; output: %s", campaignName, shellConnectOut)

	trimmed := strings.TrimSpace(shellConnectOut)
	require.Len(t, strings.Split(trimmed, "\n"), 1, "shell-connect output must be exactly one line: %q", trimmed)
	require.True(t, strings.HasPrefix(trimmed, "exec ssh -t "), "shell-connect line must start with 'exec ssh -t ': %q", trimmed)
	require.Contains(t, trimmed, "cd ", "shell-connect line must cd into the resolved root: %q", trimmed)
	require.Contains(t, trimmed, root, "shell-connect line must reference the resolved remote campaign root %q: %q", root, trimmed)

	// ResolveRoot's ssh call (the one that produced `root` above, over the
	// wire via the remote's own `camp switch --print`) must have created the
	// per-machine ControlMaster socket.
	const socketPath = "/root/.obey/ssh-ctl/self.sock"
	_, socketExitCode, err := tc.ExecCommand("test", "-S", socketPath)
	require.NoError(t, err)
	require.Equal(t, 0, socketExitCode, "ControlMaster socket %s should exist after resolve", socketPath)

	// --- Execute the hop for real (the non-`exec` variant, so the test
	// process survives) using the same ssh options camp emits, and confirm it
	// lands in the resolved campaign root.
	hopCmd := fmt.Sprintf(
		`ssh -i /root/.ssh/id_ed25519 -o StrictHostKeyChecking=accept-new -o BatchMode=yes `+
			`-o ControlMaster=auto -o ControlPath=%s -o ControlPersist=30s root@localhost "cd %s && pwd"`,
		socketPath, root,
	)
	hopOut := tc.Shell(t, hopCmd)
	require.Equal(t, root, strings.TrimSpace(hopOut), "hop over ssh should land in the resolved campaign root")

	// Resolve + hop must reuse ONE ControlMaster connection: exactly one
	// socket file under ~/.obey/ssh-ctl.
	countOut := tc.Shell(t, "ls /root/.obey/ssh-ctl/ | wc -l")
	require.Equal(t, "1", strings.TrimSpace(countOut), "resolve and hop should share a single ControlMaster socket")
}
