//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// provisionLoopbackSSH idempotently sets up key-based ssh to localhost inside the
// pooled container and symlinks /camp onto the PATH a non-interactive ssh session
// sees, so a machine pointed at localhost enumerates this container's own camp.
func provisionLoopbackSSH(t *testing.T, tc *TestContainer) {
	t.Helper()
	out, code, err := tc.ExecCommand("sh", "-c",
		"apk info -e openssh-server >/dev/null 2>&1 || apk add --no-cache openssh openssh-server")
	require.NoError(t, err, "apk add exec failed to run")
	if code != 0 {
		t.Skipf("cannot install openssh-server in this environment: %s", out)
	}
	tc.Shell(t, `
set -e
ssh-keygen -A >/dev/null 2>&1
mkdir -p /root/.ssh && chmod 700 /root/.ssh
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

// TestRemoteListLoopbackSmoke proves `camp list --remote` fans out over real ssh:
// a machine "self" pointed at localhost enumerates this container's own campaigns,
// re-tagged machine: self, alongside the local rows.
func TestRemoteListLoopbackSmoke(t *testing.T) {
	tc := GetSharedContainer(t)
	provisionLoopbackSSH(t, tc)

	const base = "/campaigns"
	_, err := tc.RunCamp("create", "list-remote-campaign",
		"-d", "list --remote smoke", "-m", "prove fan-out", "--no-git", "--path", base)
	require.NoError(t, err)

	machinesYAML := `version: 1
machines:
  - id: self
    label: Self (loopback)
    host: localhost
    auth_method: ssh-agent
    ssh_user: root
    identity_file: /root/.ssh/id_ed25519
`
	require.NoError(t, tc.WriteFile("/root/.obey/machines.yaml", machinesYAML))

	// --json: rows tagged machine:self (remote) and machine:local (this machine).
	jsonOut, err := tc.RunCamp("list", "--remote", "--json")
	require.NoError(t, err, "camp list --remote --json failed: %s", jsonOut)
	var rows []struct {
		Name    string `json:"name"`
		Machine string `json:"machine"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &rows), "output not JSON: %s", jsonOut)
	var haveLocal, haveSelf bool
	for _, r := range rows {
		if r.Machine == "local" {
			haveLocal = true
		}
		if r.Machine == "self" && r.Name == "list-remote-campaign" {
			haveSelf = true
		}
	}
	require.True(t, haveLocal, "expected a machine:local row: %s", jsonOut)
	require.True(t, haveSelf, "expected the loopback campaign tagged machine:self: %s", jsonOut)

	// Human table gains the MACHINE column when a remote machine is present.
	tableOut, err := tc.RunCamp("list", "--remote")
	require.NoError(t, err)
	require.Contains(t, tableOut, "MACHINE", "human --remote output missing MACHINE column: %s", tableOut)

	// Default `camp list --json` (no --remote) must NOT carry the machine key.
	plainJSON, err := tc.RunCamp("list", "--json")
	require.NoError(t, err)
	require.NotContains(t, plainJSON, `"machine"`, "default --json leaked the machine field: %s", plainJSON)
}

// TestRemoteListDegradesOnUnreachable proves a dead machine becomes a labeled
// row without dropping local rows or failing the command.
func TestRemoteListDegradesOnUnreachable(t *testing.T) {
	tc := GetSharedContainer(t)
	provisionLoopbackSSH(t, tc)

	_, err := tc.RunCamp("create", "degrade-local-campaign",
		"-d", "degradation", "-m", "local survives", "--no-git", "--path", "/campaigns")
	require.NoError(t, err)

	// A machine pointed at an unroutable address must fail fast (ConnectTimeout)
	// and not take the local rows down with it.
	machinesYAML := `version: 1
machines:
  - id: deadbox
    label: Dead
    host: 203.0.113.1
    auth_method: ssh-agent
    ssh_user: root
    identity_file: /root/.ssh/id_ed25519
`
	require.NoError(t, tc.WriteFile("/root/.obey/machines.yaml", machinesYAML))

	out, err := tc.RunCamp("list", "--remote")
	require.NoError(t, err, "list --remote must stay exit 0 despite a dead machine: %s", out)
	require.Contains(t, out, "degrade-local-campaign", "local rows must still render: %s", out)
	require.Contains(t, out, "deadbox", "unreachable machine must be labeled: %s", out)
	require.Contains(t, out, "unreachable", "unreachable marker missing: %s", out)
}
