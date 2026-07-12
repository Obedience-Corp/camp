//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// machineDiagnoseRow mirrors `camp machine diagnose --json`'s per-machine shape
// (cmd/camp/machine.go's machineDiagnoseRow).
type machineDiagnoseRow struct {
	ID     string `json:"id"`
	Socket string `json:"socket"`
	State  string `json:"state"`
	Reset  bool   `json:"reset"`
}

func diagnoseJSON(t *testing.T, tc *TestContainer, args ...string) []machineDiagnoseRow {
	t.Helper()
	out, err := tc.RunCamp(append([]string{"machine", "diagnose", "--json"}, args...)...)
	require.NoError(t, err, "camp machine diagnose --json failed: %s", out)
	var payload struct {
		Machines []machineDiagnoseRow `json:"machines"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &payload), "output not JSON: %s", out)
	return payload.Machines
}

func diagnoseRow(rows []machineDiagnoseRow, id string) (machineDiagnoseRow, bool) {
	for _, r := range rows {
		if r.ID == id {
			return r, true
		}
	}
	return machineDiagnoseRow{}, false
}

// TestMachineDiagnoseControlMasterLifecycle proves `camp machine diagnose` over a
// real loopback ssh ControlMaster: a warmed master reads back "live", --reset
// tears it down (socket file removed), and a re-diagnose reads "none". This is
// the operator recovery path for a stale multiplex socket flagged in the MM0001
// review (a socket left behind by a sleep/network flap would otherwise hang the
// next hop until ControlPersist expires).
func TestMachineDiagnoseControlMasterLifecycle(t *testing.T) {
	tc := GetSharedContainer(t)
	provisionLoopbackSSH(t, tc)

	const base = "/campaigns"
	_, err := tc.RunCamp("create", "diagnose-campaign",
		"-d", "diagnose control master", "-m", "warm a master", "--no-git", "--path", base)
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

	// Before any hop there is no socket: the machine reads "none".
	before, ok := diagnoseRow(diagnoseJSON(t, tc), "self")
	require.True(t, ok, "self should appear in diagnose output")
	require.Equal(t, "none", before.State, "no hop yet, expected none")

	// Warm a ControlMaster over real ssh: the resolve step of a remote switch
	// opens the per-machine master (ControlPersist keeps it alive briefly).
	shellConnect, err := tc.RunCamp("switch", "self:diagnose-campaign", "--shell-connect")
	require.NoError(t, err, "warming switch failed: %s", shellConnect)

	const socketPath = "/root/.obey/ssh-ctl/self.sock"
	_, code, err := tc.ExecCommand("test", "-S", socketPath)
	require.NoError(t, err)
	require.Equal(t, 0, code, "resolve should have created the ControlMaster socket %s", socketPath)

	// diagnose now sees a live master (the socket answers `ssh -O check`).
	live, ok := diagnoseRow(diagnoseJSON(t, tc, "self"), "self")
	require.True(t, ok)
	require.Equal(t, "live", live.State, "warmed master should read live")
	require.False(t, live.Reset, "read-only diagnose must not clear anything")

	// --reset only clears STALE sockets, so a live socket survives a plain reset.
	liveAfterReset, ok := diagnoseRow(diagnoseJSON(t, tc, "self", "--reset"), "self")
	require.True(t, ok)
	require.Equal(t, "live", liveAfterReset.State, "--reset must not tear down a live master")
	require.False(t, liveAfterReset.Reset)

	// Simulate the stuck-socket case: SIGKILL the master out from under its
	// socket so the file is left behind (no unlink on -9) but `ssh -O check` no
	// longer answers. The pid comes from -O check itself, so the kill is exact.
	tc.Shell(t, `
pid=$(ssh -o ControlPath=`+socketPath+` -O check root@localhost 2>&1 | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p')
[ -n "$pid" ] && kill -9 "$pid" 2>/dev/null || true
test -S `+socketPath+` || { echo "socket vanished before stale check" >&2; exit 1; }
`)

	stale, ok := diagnoseRow(diagnoseJSON(t, tc, "self"), "self")
	require.True(t, ok)
	require.Equal(t, "stale", stale.State, "socket present but master gone should read stale")

	// --reset clears the stale socket: it reports reset and removes the file.
	cleared, ok := diagnoseRow(diagnoseJSON(t, tc, "self", "--reset"), "self")
	require.True(t, ok)
	require.True(t, cleared.Reset, "stale socket should be reported as reset")
	require.Equal(t, "none", cleared.State, "after reset the state is none")

	_, code, err = tc.ExecCommand("test", "-e", socketPath)
	require.NoError(t, err)
	require.NotEqual(t, 0, code, "reset should have removed the stale socket file %s", socketPath)

	// A final diagnose confirms the machine is back to none.
	final, ok := diagnoseRow(diagnoseJSON(t, tc), "self")
	require.True(t, ok)
	require.Equal(t, "none", final.State)
}
