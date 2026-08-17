//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRemoteFishLoginShellFindsCampOffPath is the fish half of the far-side
// resolver's proof (internal/remote's shell round-trip tests cover sh, dash,
// bash, and zsh). It provisions a real sshd account whose passwd shell is
// fish, whose login-shell PATH has no camp anywhere, and whose camp lives
// only in ~/go/bin (the stock `go install` layout). sshd hands the generated
// remote command line to `fish -c`, fish re-enters itself with -lc, and the
// resolver must still find and run that camp: `camp machine diagnose --json`
// reports the fallback path, and `camp list --remote` returns no error row
// for the machine.
//
// The account is blinded through config.fish (which every fish instance
// reads) because the shared container symlinks /camp into /usr/bin for
// root; a stock Linux account is blind for the same reason in a different
// file (its PATH line lives in an interactive-only rc), and the mechanism
// under test — login shell cannot see camp, resolver falls back — is
// identical.
func TestRemoteFishLoginShellFindsCampOffPath(t *testing.T) {
	tc := GetSharedContainer(t)
	provisionLoopbackSSH(t, tc)

	out, code, err := tc.ExecCommand("sh", "-c",
		"apk info -e fish >/dev/null 2>&1 || apk add --no-cache fish")
	require.NoError(t, err, "apk add exec failed to run")
	if code != 0 {
		t.Skipf("cannot install fish in this environment: %s", out)
	}

	tc.Shell(t, `
set -e
id fishy >/dev/null 2>&1 || adduser -D -s /usr/bin/fish -h /home/fishy fishy
sed -i 's/^fishy:!/fishy:*/' /etc/shadow
mkdir -p /home/fishy/.ssh /home/fishy/go/bin /home/fishy/.config/fish
cat /root/.ssh/id_ed25519.pub > /home/fishy/.ssh/authorized_keys
chmod 700 /home/fishy/.ssh
chmod 600 /home/fishy/.ssh/authorized_keys
cp /camp /home/fishy/go/bin/camp
printf 'set -gx PATH /bin\n' > /home/fishy/.config/fish/config.fish
chown -R fishy:fishy /home/fishy
`)

	// The account really is fish, and really cannot see camp on its own.
	probe := tc.Shell(t, `
out=""
for i in 1 2 3 4 5; do
  out=$(ssh -i /root/.ssh/id_ed25519 -o StrictHostKeyChecking=accept-new -o BatchMode=yes fishy@localhost 'echo SHELL=$SHELL; command -v camp; or echo NOCAMP' 2>&1) && break
  sleep 1
done
echo "$out"
`)
	require.Contains(t, probe, "SHELL=/usr/bin/fish", "fishy account is not a fish login: %s", probe)
	require.Contains(t, probe, "NOCAMP", "fishy account can see camp on its own PATH; the test would prove nothing: %s", probe)

	machinesYAML := `version: 1
machines:
  - id: fishy
    label: Fish login shell, camp only in ~/go/bin
    host: localhost
    auth_method: ssh-agent
    ssh_user: fishy
    identity_file: /root/.ssh/id_ed25519
`
	require.NoError(t, tc.WriteFile("/root/.obey/machines.yaml", machinesYAML))

	diagOut, err := tc.RunCamp("machine", "diagnose", "--json")
	require.NoError(t, err, "camp machine diagnose --json failed: %s", diagOut)
	var diag struct {
		Machines []struct {
			ID          string `json:"id"`
			CampVersion string `json:"camp_version"`
			CampPath    string `json:"camp_path"`
			CampOnPath  bool   `json:"camp_on_path"`
			CampMissing bool   `json:"camp_missing"`
			CheckURL    string `json:"check_url"`
		} `json:"machines"`
	}
	require.NoError(t, json.Unmarshal([]byte(diagOut), &diag), "diagnose output not JSON: %s", diagOut)
	require.Len(t, diag.Machines, 1, "diagnose rows: %s", diagOut)
	row := diag.Machines[0]
	require.Equal(t, "fishy", row.ID)
	require.False(t, row.CampMissing, "resolver reported camp missing under fish: %s", diagOut)
	require.Equal(t, "/home/fishy/go/bin/camp", row.CampPath, "resolver did not fall back to ~/go/bin under fish: %s", diagOut)
	require.False(t, row.CampOnPath, "login-shell PATH was supposed to be blind: %s", diagOut)
	require.NotEmpty(t, row.CampVersion, "version probe did not run against the resolved camp: %s", diagOut)

	// The list fan-out runs the same command line; the machine must not
	// degrade to an error row.
	stdout, stderr, exitCode, err := tc.RunCampSplit("list", "--remote")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "camp list --remote exit %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	require.NotContains(t, stdout, "(camp not found:", "fish machine degraded to camp-not-found:\n%s", stdout)
	require.NotContains(t, stdout, "(unreachable:", "fish machine degraded to unreachable:\n%s", stdout)
	require.NotContains(t, stderr, "fishy", "list --remote warned about the fish machine: %s", stderr)
}
