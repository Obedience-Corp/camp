//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These helpers stand up a loopback ssh "peer" inside the pooled container so
// the peer transports (camp sync/clone --from <machine>, artifact rsync pull)
// can be exercised end to end over a real ssh hop — the path unit tests skip
// entirely because they construct a filesystem peer.Source (peer.FromPath) and
// never dial ssh.
//
// A genuine A->B transfer needs the source and destination to be DIFFERENT
// campaign roots. camp resolves --from by running the peer's own
// `camp switch <name> --print`, which reads the peer login user's HOME
// registry. Root and an unprivileged `peer` user therefore give two distinct
// registries, so one campaign name resolves to two different paths — root's
// local checkout and peer's source checkout. Reusing root for both ends would
// resolve one name to one path and make every transfer a no-op self-copy.
const (
	peerUser          = "peer"
	peerHome          = "/home/peer"
	peerCampaignsDir  = "/home/peer/campaigns"
	loopbackMachineID = "self"
	rootIdentity      = "/root/.ssh/id_ed25519"
)

// shQuote single-quotes s for safe embedding in a POSIX shell command, matching
// remote.ShellQuote. Applying it at each nesting layer (root sh -lc -> ssh ->
// peer sh -lc) keeps arbitrary script text intact through the hops.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ensureSSHDaemon provisions and starts sshd (plus rsync) in the pooled
// container, idempotently, and generates root's loopback key so key auth to
// localhost works. Every step is check-then-act: Reset() does not uninstall
// packages or kill processes, so a later test reusing this same pooled
// container must not redo (or break) work an earlier one already did.
//
// It Skips (not fails) when openssh cannot be installed, so the suite still runs
// in a sandbox with no package mirror.
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

	// sshd daemonizes on start; retry briefly so the sanity check is not racing
	// the listener coming up.
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

// ensurePeerAccount adds an unprivileged `peer` login user (idempotently) that
// accepts root's key, so a `self` machine with ssh_user=peer reaches a distinct
// HOME — and thus a distinct camp registry — from root's. It builds on
// ensureSSHDaemon and verifies both that root can log in as peer and that camp
// is on peer's login-shell PATH (the exact channel ResolveRoot uses).
func ensurePeerAccount(t *testing.T, tc *TestContainer) {
	t.Helper()
	ensureSSHDaemon(t, tc)

	// The user, keys, and git identity are durable/idempotent (Reset() never
	// touches /home/peer), but the peer registry and its campaigns must start
	// clean every test, or a name a prior test registered would still resolve.
	// Campaigns live under a dedicated dir so wiping registry state never
	// endangers .ssh or .gitconfig.
	tc.Shell(t, fmt.Sprintf(`
set -e
id %[1]s >/dev/null 2>&1 || adduser -D -s /bin/sh -h %[2]s %[1]s
# adduser -D leaves the account LOCKED ('!' in /etc/shadow); OpenSSH refuses
# pubkey auth for a locked account. Flip it to '*' (unlocked, still no
# password login) so key auth is accepted while password login stays off.
sed -i 's/^%[1]s:!/%[1]s:*/' /etc/shadow
mkdir -p %[2]s/.ssh
cat /root/.ssh/id_ed25519.pub > %[2]s/.ssh/authorized_keys
chmod 700 %[2]s/.ssh
chmod 600 %[2]s/.ssh/authorized_keys
# camp runs git for any campaign op; give peer a committer identity.
su %[1]s -c 'git config --global user.email peer@test.com && git config --global user.name Peer'
rm -rf %[2]s/.obey %[2]s/.config %[3]s
mkdir -p %[3]s
# sshd StrictModes: peer must own its HOME, .ssh, and authorized_keys, and
# none may be group/world-writable.
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

// registerLoopbackMachine writes root's ~/.obey/machines.yaml with a `self`
// machine that ssh's to peer@localhost with root's key. From root's camp,
// `--from self` therefore resolves campaigns in PEER's registry.
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

// peerSSH runs script on the peer account over loopback ssh through a login
// shell (sh -lc), the same way camp's own ResolveRoot reaches a peer, and
// returns combined output. It fails the test on a non-zero exit. Use it to set
// up the peer-side campaign (created in PEER's registry) that root then pulls
// from.
func peerSSH(t *testing.T, tc *TestContainer, script string) string {
	t.Helper()
	remote := "sh -lc " + shQuote(script)
	full := fmt.Sprintf(
		"ssh -i %s -o StrictHostKeyChecking=accept-new -o BatchMode=yes %s@localhost %s",
		rootIdentity, peerUser, shQuote(remote))
	return tc.Shell(t, full)
}
