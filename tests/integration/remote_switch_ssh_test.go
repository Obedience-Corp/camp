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

	// --- Provision sshd idempotently. ------------------------------------
	// The pooled container is shared across tests and Reset() does not
	// uninstall packages or kill processes (it only clears /test,
	// /campaigns, /root/.obey*, /root/.camp). So package install, host key
	// generation, and starting sshd must all check-then-act, or a later test
	// reusing this same container would redo (and potentially break) work
	// that already succeeded.
	apkOut, apkExitCode, apkErr := tc.ExecCommand("sh", "-c",
		"apk info -e openssh-server >/dev/null 2>&1 || apk add --no-cache openssh openssh-server")
	require.NoError(t, apkErr, "apk add exec failed to run")
	if apkExitCode != 0 {
		t.Skipf("cannot install openssh-server in this container environment: %s", apkOut)
	}

	tc.Shell(t, `
set -e
ssh-keygen -A >/dev/null 2>&1
mkdir -p /root/.ssh
chmod 700 /root/.ssh
[ -f /root/.ssh/id_ed25519 ] || ssh-keygen -t ed25519 -N '' -f /root/.ssh/id_ed25519 >/dev/null
cat /root/.ssh/id_ed25519.pub > /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
if grep -q '^PermitRootLogin' /etc/ssh/sshd_config; then
  sed -i 's/^PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config
else
  printf '%s\n' 'PermitRootLogin yes' >> /etc/ssh/sshd_config
fi
ln -sf /camp /usr/bin/camp
pgrep sshd >/dev/null 2>&1 || /usr/sbin/sshd
`)

	// Sanity-check loopback ssh actually works before trusting camp's hop.
	// sshd daemonizes on start, so retry briefly in case it isn't accepting
	// connections the instant the provisioning script returns.
	sshCheck := tc.Shell(t, `
out=""
for i in 1 2 3 4 5; do
  out=$(ssh -i /root/.ssh/id_ed25519 -o StrictHostKeyChecking=accept-new -o BatchMode=yes root@localhost 'echo SSHOK' 2>&1) && break
  sleep 1
done
echo "$out"
`)
	require.Contains(t, sshCheck, "SSHOK", "loopback ssh sanity check failed: %s", sshCheck)

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
