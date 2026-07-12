//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// machineJSONRow mirrors `camp machine list --json`'s per-machine shape
// (cmd/camp/machine.go's machineJSON): the ~/.obey/machines.yaml field names,
// not Go's default exported-field JSON encoding.
type machineJSONRow struct {
	ID           string `json:"id"`
	Label        string `json:"label,omitempty"`
	Host         string `json:"host,omitempty"`
	AuthMethod   string `json:"auth_method,omitempty"`
	SSHUser      string `json:"ssh_user,omitempty"`
	IdentityFile string `json:"identity_file,omitempty"`
}

type machineListPayload struct {
	Version  int              `json:"version"`
	Machines []machineJSONRow `json:"machines"`
}

func machineListJSON(t *testing.T, tc *TestContainer) machineListPayload {
	t.Helper()
	out, err := tc.RunCamp("machine", "list", "--json")
	require.NoError(t, err, "camp machine list --json failed: %s", out)
	var payload machineListPayload
	require.NoError(t, json.Unmarshal([]byte(out), &payload), "output not JSON: %s", out)
	return payload
}

func findMachine(rows []machineJSONRow, id string) (machineJSONRow, bool) {
	for _, r := range rows {
		if r.ID == id {
			return r, true
		}
	}
	return machineJSONRow{}, false
}

// TestMachineListEmptyShowsOnlyLocal proves the Step-2 decision from
// 01_machine_subcommands.md: on a fresh container with no ~/.obey/machines.yaml,
// `camp machine list --json` lists exactly the synthetic {"id":"local"} row
// (the file's own Load() degrades a missing file to zero machines).
func TestMachineListEmptyShowsOnlyLocal(t *testing.T) {
	tc := GetSharedContainer(t)

	payload := machineListJSON(t, tc)
	require.Len(t, payload.Machines, 1, "expected only the synthetic local row: %+v", payload.Machines)
	require.Equal(t, "local", payload.Machines[0].ID)
	require.Empty(t, payload.Machines[0].Host, "synthetic local row must not carry other fields")
}

// TestMachineAddListRemove proves the full add/list/remove lifecycle over the
// real CLI in-container, including the two "must error" guards (reserved
// "local" id, invalid --auth) and add's idempotency on id (no duplicate row on
// a second add of the same id).
func TestMachineAddListRemove(t *testing.T) {
	tc := GetSharedContainer(t)

	out, err := tc.RunCamp("machine", "add", "devbox", "--host", "devbox.ts.net", "--auth", "ssh-agent")
	require.NoError(t, err, "machine add failed: %s", out)

	payload := machineListJSON(t, tc)
	require.Len(t, payload.Machines, 2, "expected local + devbox: %+v", payload.Machines)
	row, ok := findMachine(payload.Machines, "devbox")
	require.True(t, ok, "devbox missing from list --json: %+v", payload.Machines)
	require.Equal(t, "devbox.ts.net", row.Host)
	require.Equal(t, "ssh-agent", row.AuthMethod)

	// Re-adding the same id updates the existing row in place; no duplicate.
	out, err = tc.RunCamp("machine", "add", "devbox", "--host", "devbox2.ts.net")
	require.NoError(t, err, "machine add (update) failed: %s", out)
	payload = machineListJSON(t, tc)
	require.Len(t, payload.Machines, 2, "update duplicated the row instead of replacing it: %+v", payload.Machines)
	row, ok = findMachine(payload.Machines, "devbox")
	require.True(t, ok)
	require.Equal(t, "devbox2.ts.net", row.Host, "update did not take effect")

	out, err = tc.RunCamp("machine", "add", "local", "--host", "x")
	require.Error(t, err, "machine add local should fail: %s", out)

	out, err = tc.RunCamp("machine", "add", "badauth", "--host", "x", "--auth", "bogus")
	require.Error(t, err, "machine add with invalid --auth should fail: %s", out)
	require.Contains(t, out, "ssh-agent", "invalid --auth error should list valid values: %s", out)

	out, err = tc.RunCamp("machine", "remove", "devbox")
	require.NoError(t, err, "machine remove failed: %s", out)
	payload = machineListJSON(t, tc)
	_, ok = findMachine(payload.Machines, "devbox")
	require.False(t, ok, "devbox still present after remove: %+v", payload.Machines)
	require.Len(t, payload.Machines, 1, "expected only local after remove: %+v", payload.Machines)

	out, err = tc.RunCamp("machine", "remove", "devbox")
	require.Error(t, err, "machine remove of an unknown id should fail: %s", out)

	out, err = tc.RunCamp("machine", "remove", "local")
	require.Error(t, err, "machine remove local should fail: %s", out)
}

// TestMachineAddRoundTripsAllFields proves the machines-library atomic
// Save->Load real-filesystem round trip that phase 001 deferred to this task:
// every one of the six schema fields set on `add` survives through
// Load->Upsert->Save and back out through a fresh `list --json`.
func TestMachineAddRoundTripsAllFields(t *testing.T) {
	tc := GetSharedContainer(t)

	out, err := tc.RunCamp("machine", "add", "fullbox",
		"--host", "fullbox.ts.net",
		"--label", "Full Box",
		"--auth", "ssh-password",
		"--user", "opsuser",
		"--identity", "/root/.ssh/id_ed25519",
	)
	require.NoError(t, err, "machine add failed: %s", out)

	payload := machineListJSON(t, tc)
	row, ok := findMachine(payload.Machines, "fullbox")
	require.True(t, ok, "fullbox missing from list --json: %+v", payload.Machines)
	require.Equal(t, "fullbox", row.ID)
	require.Equal(t, "Full Box", row.Label)
	require.Equal(t, "fullbox.ts.net", row.Host)
	require.Equal(t, "ssh-password", row.AuthMethod)
	require.Equal(t, "opsuser", row.SSHUser)
	require.Equal(t, "/root/.ssh/id_ed25519", row.IdentityFile)

	raw, err := tc.ReadFile("/root/.obey/machines.yaml")
	require.NoError(t, err)
	require.Contains(t, raw, "fullbox.ts.net")
	require.Contains(t, raw, "opsuser")
	require.Contains(t, raw, "/root/.ssh/id_ed25519")
}
