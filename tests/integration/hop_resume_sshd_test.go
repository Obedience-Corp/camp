//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// setSSHDForwarding sets AllowTcpForwarding on the loopback sshd, which also
// governs remote unix-socket forwards, and waits for the reloaded server.
func setSSHDForwarding(t *testing.T, tc *TestContainer, allow string) {
	t.Helper()
	out := tc.Shell(t, fmt.Sprintf(`
set -e
if grep -q '^AllowTcpForwarding' /etc/ssh/sshd_config; then
  sed -i 's/^AllowTcpForwarding.*/AllowTcpForwarding %[1]s/' /etc/ssh/sshd_config
else
  printf '%%s\n' 'AllowTcpForwarding %[1]s' >> /etc/ssh/sshd_config
fi
kill -HUP "$(cat /run/sshd.pid)"
for i in 1 2 3 4 5 6 7 8 9 10; do
  ssh -i %[2]s -o StrictHostKeyChecking=accept-new -o BatchMode=yes root@localhost 'echo SSHOK' 2>/dev/null && break
  sleep 0.5
done
/usr/sbin/sshd -T 2>/dev/null | grep -i '^allowtcpforwarding'
`, allow, rootIdentity))
	require.Contains(t, out, "SSHOK", "sshd did not come back after reload: %s", out)
	require.Contains(t, out, "allowtcpforwarding "+allow, "sshd did not take the setting: %s", out)
}

// TestHopResumeOverRealSSHD drives a real hop through OpenSSH, with a tty on
// the opening side, and has the far shell ask to switch onward twice. The
// opening shell has no route to the first host named, so it must refuse, and
// the far shell must show that refusal and stay. It has a route to the second:
// the far shell is told to go, and when the hop ends the opening shell is left
// with the ssh line for that destination.
//
// The hop line carries the multiplexing options camp's own lines carry, so the
// relay runs as a second session on the hop's ControlMaster, whether the hop
// creates that master or finds one up.
//
// What a hopped shell may trust depends on who it is and on what the ssh
// server did with the forwarded socket, and only a real sshd shows the latter:
//   - where the server forwards it, an ordinary user owns the socket in /tmp,
//     so the released socket route carries the request and the relay steps
//     aside;
//   - root does not trust a shared directory, so its request goes over the
//     relay, started on a second session of the same server;
//   - where the server refuses the forward (Alpine's default configuration
//     does), there is no socket at all and the relay is the only route.
func TestHopResumeOverRealSSHD(t *testing.T) {
	tc := GetSharedContainer(t)
	ensurePeerAccount(t, tc)
	const relayDirs = "/root/.obey/hop-resume " + peerHome + "/.obey/hop-resume"
	t.Cleanup(func() {
		setSSHDForwarding(t, tc, "no")
		// sshd leaves the sockets it forwarded behind.
		tc.Shell(t, "rm -rf /tmp/camp-hr-* "+relayDirs)
	})

	// Where the onward switch goes: a campaign the opening shell reaches on a
	// machine it knows by the host the far shell will name.
	const onward = "hop-onward-campaign"
	createOut, err := tc.RunCamp("create", onward,
		"-d", "hop resume onward switch",
		"-m", "prove a switch is carried back across a hop",
		"--no-git",
		"--path", "/campaigns",
	)
	require.NoError(t, err, "camp create: %s", createOut)
	printOut, err := tc.RunCamp("switch", onward, "--print")
	require.NoError(t, err, "camp switch --print: %s", printOut)
	onwardRoot := strings.TrimSpace(printOut)
	require.NotEmpty(t, onwardRoot)
	require.NoError(t, tc.WriteFile("/root/.obey/machines.yaml", fmt.Sprintf(`version: 1
machines:
  - id: onwardbox
    label: Onward destination (hop resume test)
    host: localhost
    auth_method: ssh-agent
    ssh_user: root
    identity_file: %s
`, rootIdentity)))

	// Long enough for the relay, which starts a moment after the hop, to be
	// listening or to have stepped aside. The far shell knows the destination
	// by an alias of its own; the host is what the opening shell goes by.
	const farShell = `sleep 3; camp hop-resume send --host elsewhere.example thirdbox:notes; ` +
		`echo SEND_EXIT=$?; ` +
		`camp hop-resume send --host localhost thirdbox:` + onward + `; ` +
		`echo ONWARD_EXIT=$?; ` +
		`echo RELAY_SOCKETS=$(ls "$HOME/.obey/hop-resume"/*.sock 2>/dev/null | wc -l); ` +
		`echo SOCK_OWNER=$(stat -c %U "$CAMP_HOP_RESUME_SOCK" 2>/dev/null || echo none)`

	tc.Shell(t, "mkdir -p /root/.obey/ssh-ctl")
	for i, hop := range []struct {
		name       string
		forwarding string
		user       string
		relays     string
		sockOwner  string
		masterUp   bool // a ControlMaster is already up when the hop starts
	}{
		{"forward refused, ordinary user over the relay", "no", peerUser, "RELAY_SOCKETS=1", "SOCK_OWNER=none", false},
		{"ordinary user over the forwarded socket", "yes", peerUser, "RELAY_SOCKETS=0", "SOCK_OWNER=" + peerUser, false},
		{"ordinary user over the forwarded socket, master already up", "yes", peerUser, "RELAY_SOCKETS=0", "SOCK_OWNER=" + peerUser, true},
		{"root over the relay", "yes", "root", "RELAY_SOCKETS=1", "SOCK_OWNER=root", false},
		{"root over the relay, master already up", "yes", "root", "RELAY_SOCKETS=1", "SOCK_OWNER=root", true},
	} {
		t.Run(hop.name, func(t *testing.T) {
			setSSHDForwarding(t, tc, hop.forwarding)
			// A master of its own per case: one that outlived an earlier case
			// would carry that case's server settings into this one.
			options := ""
			for _, token := range []string{
				"-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=8",
				"-o", "ControlMaster=auto", "-o", fmt.Sprintf("ControlPath=/root/.obey/ssh-ctl/hop-resume-test-%d.sock", i),
				"-o", "ControlPersist=30s", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-i", rootIdentity,
			} {
				options += " " + shQuote(token)
			}
			target := shQuote(hop.user + "@localhost")
			t.Cleanup(func() { tc.Shell(t, "ssh"+options+" -O exit "+target+" 2>/dev/null || true") })
			if hop.masterUp {
				up := tc.Shell(t, "ssh"+options+" "+target+" 'echo MASTEROK' 2>&1; ssh"+options+" -O check "+target+" 2>&1 || true")
				require.Contains(t, up, "MASTEROK", "could not bring a master up: %s", up)
				require.Contains(t, up, "Master running", "the master did not persist: %s", up)
			}
			line := "ssh -t" + options + " " + target + " " + shQuote(farShell)
			out, err := tc.RunCampInteractiveStepsInDirTimeout("/root", 90*time.Second,
				[]InteractiveStep{{WaitFor: "SOCK_OWNER=", WaitTimeout: 60 * time.Second}},
				"hop-resume", "run", "--", line)
			require.NoError(t, err, "hop failed:\n%s", out)

			require.Contains(t, out, "parent has no route to elsewhere.example",
				"the far shell must show the opening shell's answer:\n%s", out)
			require.Contains(t, out, "SEND_EXIT=1", "a refused switch must leave the far shell running:\n%s", out)
			require.Contains(t, out, "ONWARD_EXIT=0", "a switch the opening shell can make must be accepted:\n%s", out)
			require.Contains(t, out, hop.sockOwner, "forwarded socket:\n%s", out)
			require.Contains(t, out, hop.relays, "relay presence:\n%s", out)
			for _, refused := range []string{"only this user could have put there", "runs as another user", "relay for this hop is not running"} {
				require.False(t, strings.Contains(out, refused), "the route was refused (%q):\n%s", refused, out)
			}
			// What the hop leaves the opening shell to run once it has ended.
			_, afterHop, _ := strings.Cut(out, "SOCK_OWNER=")
			require.Contains(t, afterHop, "ssh -t ", "the opening shell must be left with the onward switch:\n%s", out)
			require.Contains(t, afterHop, onwardRoot, "the onward switch must go to the campaign asked for:\n%s", out)

			left := tc.Shell(t, `for i in 1 2 3 4 5 6 7 8 9 10; do
  n=$(find `+relayDirs+` -mindepth 1 2>/dev/null | grep -c .)
  [ "$n" = 0 ] && break
  sleep 0.5
done
echo LEFT=$n`)
			require.Contains(t, left, "LEFT=0", "an ended hop must leave nothing in the relay directory: %s", left)
		})
	}
}
