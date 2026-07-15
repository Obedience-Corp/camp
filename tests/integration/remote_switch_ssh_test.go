//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRemoteSwitchLocalhostSshSmoke verifies the loopback SSH hop and its
// ControlMaster reuse.
func TestRemoteSwitchLocalhostSshSmoke(t *testing.T) {
	tc := GetSharedContainer(t)

	// Provision shared loopback SSH state.
	ensureSSHDaemon(t, tc)

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

	shellConnectOut, err := tc.RunCamp("switch", "self:"+campaignName, "--shell-connect")
	require.NoError(t, err, "camp switch self:%s --shell-connect should succeed; output: %s", campaignName, shellConnectOut)

	trimmed := strings.TrimSpace(shellConnectOut)
	require.Len(t, strings.Split(trimmed, "\n"), 1, "shell-connect output must be exactly one line: %q", trimmed)
	require.True(t, strings.HasPrefix(trimmed, "exec ssh -t "), "shell-connect line must start with 'exec ssh -t ': %q", trimmed)
	require.Contains(t, trimmed, "cd ", "shell-connect line must cd into the resolved root: %q", trimmed)
	require.Contains(t, trimmed, root, "shell-connect line must reference the resolved remote campaign root %q: %q", root, trimmed)

	const socketPath = "/root/.obey/ssh-ctl/self.sock"
	_, socketExitCode, err := tc.ExecCommand("test", "-S", socketPath)
	require.NoError(t, err)
	require.Equal(t, 0, socketExitCode, "ControlMaster socket %s should exist after resolve", socketPath)

	hopCmd := fmt.Sprintf(
		`ssh -i /root/.ssh/id_ed25519 -o StrictHostKeyChecking=accept-new -o BatchMode=yes `+
			`-o ControlMaster=auto -o ControlPath=%s -o ControlPersist=30s root@localhost "cd %s && pwd"`,
		socketPath, root,
	)
	hopOut := tc.Shell(t, hopCmd)
	require.Equal(t, root, strings.TrimSpace(hopOut), "hop over ssh should land in the resolved campaign root")

	countOut := tc.Shell(t, "ls /root/.obey/ssh-ctl/ | wc -l")
	require.Equal(t, "1", strings.TrimSpace(countOut), "resolve and hop should share a single ControlMaster socket")
}
