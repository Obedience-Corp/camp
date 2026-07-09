//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPlainListMakesNoSshCall is the acceptance guarantee that multi-machine is
// pure enrichment: with a machines file PRESENT but no --remote, `camp list`
// makes zero ssh calls (the structural --remote gate, not the absence of a
// machines file, is what keeps the local path network-free) and `camp list
// --json` stays byte-identical (no machine key). Verified with an ssh shim on
// PATH that records any invocation.
func TestPlainListMakesNoSshCall(t *testing.T) {
	tc := GetSharedContainer(t)

	tc.Shell(t, `
set -e
mkdir -p /shims
cat > /shims/ssh <<'EOF'
#!/bin/sh
echo "ssh-called $@" >> /tmp/ssh-invocations.log
exit 0
EOF
chmod +x /shims/ssh
rm -f /tmp/ssh-invocations.log
`)

	_, err := tc.RunCamp("create", "no-remote-campaign",
		"-d", "no-remote inert", "-m", "prove no ssh", "--no-git", "--path", "/campaigns")
	require.NoError(t, err)

	// A machines file EXISTS: the only reason not to ssh is the --remote gate.
	require.NoError(t, tc.WriteFile("/root/.obey/machines.yaml",
		"version: 1\nmachines:\n  - id: ghost\n    host: 203.0.113.9\n    auth_method: ssh-agent\n"))

	// Plain `camp list` and `camp list --json`, with the ssh shim first in PATH.
	listOut := tc.Shell(t, "PATH=/shims:$PATH /camp list 2>&1")
	require.Contains(t, listOut, "no-remote-campaign", "plain list should still show local campaigns")

	jsonOut := tc.Shell(t, "PATH=/shims:$PATH /camp list --json 2>&1")
	require.NotContains(t, jsonOut, `"machine"`, "default --json must not carry the machine field")

	// ssh must never have been invoked.
	_, code, _ := tc.ExecCommand("test", "-f", "/tmp/ssh-invocations.log")
	require.NotEqual(t, 0, code, "plain `camp list` must make zero ssh calls (shim recorded an invocation)")
}
