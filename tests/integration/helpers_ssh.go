//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	peerUser          = "peer"
	peerHome          = "/home/peer"
	peerCampaignsDir  = "/home/peer/campaigns"
	loopbackMachineID = "self"
	rootIdentity      = "/root/.ssh/id_ed25519"
)

// shQuote quotes a string for a POSIX shell command.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ensureSSHDaemon provisions the loopback SSH and rsync services.
func ensureSSHDaemon(t *testing.T, tc *TestContainer) {
	t.Helper()

	apkOut, apkExit, apkErr := tc.ExecCommand("sh", "-c",
		"apk info -e openssh-server >/dev/null 2>&1 && apk info -e rsync >/dev/null 2>&1 || "+
			"apk add --no-cache openssh openssh-server rsync")
	require.NoError(t, apkErr, "apk add exec failed to run")
	if apkExit != 0 {
		t.Skipf("cannot install openssh-server/rsync in this container environment: %s", apkOut)
	}

	tc.Shell(t, `
set -e
ssh-keygen -A >/dev/null 2>&1
git config --global --add safe.directory '*'
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

	sshCheck := tc.Shell(t, `
out=""
for i in 1 2 3 4 5; do
  out=$(ssh -i /root/.ssh/id_ed25519 -o StrictHostKeyChecking=accept-new -o BatchMode=yes root@localhost 'echo SSHOK' 2>&1) && break
  sleep 1
done
echo "$out"
`)
	require.Contains(t, sshCheck, "SSHOK", "loopback ssh sanity check failed: %s", sshCheck)
}

// ensurePeerAccount provisions the isolated peer login used by transfer tests.
func ensurePeerAccount(t *testing.T, tc *TestContainer) {
	t.Helper()
	ensureSSHDaemon(t, tc)

	tc.Shell(t, fmt.Sprintf(`
set -e
id %[1]s >/dev/null 2>&1 || adduser -D -s /bin/sh -h %[2]s %[1]s
sed -i 's/^%[1]s:!/%[1]s:*/' /etc/shadow
mkdir -p %[2]s/.ssh
cat /root/.ssh/id_ed25519.pub > %[2]s/.ssh/authorized_keys
chmod 700 %[2]s/.ssh
chmod 600 %[2]s/.ssh/authorized_keys
su %[1]s -c 'git config --global user.email peer@test.com && git config --global user.name Peer && git config --global --add safe.directory "*"'
rm -rf %[2]s/.obey %[2]s/.config %[3]s
mkdir -p %[3]s
chown -R %[1]s:%[1]s %[2]s
`, peerUser, peerHome, peerCampaignsDir))

	check := tc.Shell(t, fmt.Sprintf(`
out=""
for i in 1 2 3 4 5; do
  out=$(ssh -i %[1]s -o StrictHostKeyChecking=accept-new -o BatchMode=yes %[2]s@localhost 'sh -lc "command -v camp >/dev/null 2>&1 && echo PEEROK"' 2>&1) && break
  sleep 1
done
echo "$out"
`, rootIdentity, peerUser))
	require.Contains(t, check, "PEEROK", "loopback ssh to peer failed or camp not on peer PATH: %s", check)
}

// registerLoopbackMachine points the test machine at the isolated peer.
func registerLoopbackMachine(t *testing.T, tc *TestContainer) {
	t.Helper()
	yaml := fmt.Sprintf(`version: 1
machines:
  - id: %[1]s
    label: Loopback peer (integration smoke)
    host: localhost
    auth_method: ssh-agent
    ssh_user: %[2]s
    identity_file: %[3]s
`, loopbackMachineID, peerUser, rootIdentity)
	require.NoError(t, tc.WriteFile("/root/.obey/machines.yaml", yaml))
}

// peerSSH runs a setup script through the same login-shell path camp uses.
func peerSSH(t *testing.T, tc *TestContainer, script string) string {
	t.Helper()
	remote := "sh -lc " + shQuote(script)
	full := fmt.Sprintf(
		"ssh -i %s -o StrictHostKeyChecking=accept-new -o BatchMode=yes %s@localhost %s",
		rootIdentity, peerUser, shQuote(remote))
	return tc.Shell(t, full)
}
