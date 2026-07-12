//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRemoteCompletionReadsWarmCache proves the end-to-end completion path:
// `camp list --remote` warms the per-machine completion cache over real ssh, and a
// subsequent `camp __complete switch <id>:` reads that cache and offers the remote
// campaigns — with no ssh on the completion (keystroke) path.
func TestRemoteCompletionReadsWarmCache(t *testing.T) {
	tc := GetSharedContainer(t)
	provisionLoopbackSSH(t, tc)

	_, err := tc.RunCamp("create", "completion-cache-campaign",
		"-d", "completion cache", "-m", "warm then complete", "--no-git", "--path", "/campaigns")
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

	// No colon: completion offers the "self:" machine candidate.
	noColon, err := tc.RunCamp("__complete", "switch", "")
	require.NoError(t, err)
	require.Contains(t, noColon, "self:", "completion should offer the self: machine candidate: %s", noColon)

	// Before warming, "self:" is a cache miss => machine id only (no campaigns).
	preWarm, err := tc.RunCamp("__complete", "switch", "self:")
	require.NoError(t, err)
	require.NotContains(t, preWarm, "self:completion-cache-campaign",
		"campaign should not appear before the cache is warmed: %s", preWarm)

	// Warm the cache over real ssh.
	_, err = tc.RunCamp("list", "--remote", "--json")
	require.NoError(t, err)

	// Now "self:" completion reads the warm cache and offers the remote campaign.
	postWarm, err := tc.RunCamp("__complete", "switch", "self:")
	require.NoError(t, err)
	require.True(t, strings.Contains(postWarm, "self:completion-cache-campaign"),
		"completion should read the warm cache and offer the remote campaign: %s", postWarm)
}
