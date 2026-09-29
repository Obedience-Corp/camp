//go:build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func setupFixture(t *testing.T) (*TestContainer, string) {
	t.Helper()
	tc := GetSharedContainer(t)
	require.True(t, festAvailable, "setup tests require the real fest binary")
	// Per-test state also proves setup respects XDG_CONFIG_HOME and configured
	// camps directories rather than using the container's ordinary registry.
	tc.Shell(t, `rm -rf /tmp/starter-home /tmp/failing-fest
mkdir -p /tmp/starter-home/config/obey/campaign
printf '%s' '{"campaigns_dir":"/tmp/starter-home/camps"}' > /tmp/starter-home/config/obey/campaign/config.json`)
	return tc, "env -u CAMP_ROOT -u CAMP_REGISTRY_PATH HOME=/tmp/starter-home XDG_CONFIG_HOME=/tmp/starter-home/config /camp "
}

func setupResult(t *testing.T, tc *TestContainer, prefix string) map[string]any {
	t.Helper()
	out := tc.Shell(t, prefix+"setup --json")
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result), out)
	return result
}

func TestStarterSetupFreshAndDeletion(t *testing.T) {
	tc, prefix := setupFixture(t)
	result := setupResult(t, tc, prefix)
	require.Equal(t, "created", result["action"])
	require.Equal(t, "/tmp/starter-home/camps/festival", result["path"])
	tc.Shell(t, `test -d /tmp/starter-home/camps/festival/.git
test -d /tmp/starter-home/camps/festival/festivals/.festival
test -f /tmp/starter-home/camps/festival/.agents/skills/camp-navigation/SKILL.md`)
	marker, err := tc.ReadFile("/tmp/starter-home/camps/festival/festivals/.festival/.state/.workspace")
	require.NoError(t, err)
	require.Contains(t, marker, `"workspace": "festival"`)
	out := tc.Shell(t, prefix+"list --format json")
	require.Contains(t, out, `"name": "festival"`)
	require.Equal(t, "complete", setupResult(t, tc, prefix)["action"])
	tc.Shell(t, `rm -rf /tmp/starter-home/camps/festival
printf '%s' '{"version":2,"campaigns":{}}' > /tmp/starter-home/config/obey/campaign/registry.json`)
	require.Equal(t, "complete", setupResult(t, tc, prefix)["action"])
	tc.Shell(t, "test ! -e /tmp/starter-home/camps/festival")
}

func TestStarterSetupExistingAndUnregistered(t *testing.T) {
	for _, registered := range []bool{true, false} {
		t.Run(map[bool]string{true: "registered", false: "unregistered"}[registered], func(t *testing.T) {
			tc, prefix := setupFixture(t)
			args := "create festival -d 'Keep description' -m 'Keep mission'"
			if !registered {
				args = "init /tmp/starter-home/camps/festival --name festival -d 'Keep description' -m 'Keep mission' --no-register"
			}
			tc.Shell(t, prefix+args)
			before, err := tc.ReadFile("/tmp/starter-home/camps/festival/.campaign/campaign.yaml")
			require.NoError(t, err)
			expected := "existing"
			if !registered {
				expected = "registered"
			}
			require.Equal(t, expected, setupResult(t, tc, prefix)["action"])
			after, err := tc.ReadFile("/tmp/starter-home/camps/festival/.campaign/campaign.yaml")
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestStarterSetupConflictAndCorruptRegistry(t *testing.T) {
	tc, prefix := setupFixture(t)
	tc.Shell(t, "mkdir -p /tmp/starter-home/camps/festival; echo precious > /tmp/starter-home/camps/festival/mine.txt")
	result := setupResult(t, tc, prefix)
	require.Equal(t, "skipped", result["action"])
	require.Contains(t, result["message"], "/tmp/starter-home/camps/festival")
	tc.Shell(t, "test ! -e /tmp/starter-home/config/obey/campaign/starter.json; test \"$(cat /tmp/starter-home/camps/festival/mine.txt)\" = precious")
	tc.Shell(t, "echo broken > /tmp/starter-home/config/obey/campaign/registry.json")
	out, code, err := tc.ExecCommand("sh", "-c", prefix+"setup --json")
	require.NoError(t, err)
	require.NotZero(t, code, out)
	require.Contains(t, out, "registry")
}

func TestStarterSetupConcurrent(t *testing.T) {
	tc, prefix := setupFixture(t)
	tc.Shell(t, prefix+"setup --json > /tmp/first.json &\n"+prefix+"setup --json > /tmp/second.json &\nwait")
	first, err := tc.ReadFile("/tmp/first.json")
	require.NoError(t, err)
	second, err := tc.ReadFile("/tmp/second.json")
	require.NoError(t, err)
	require.Contains(t, first+second, `"action":"created"`)
	require.Contains(t, first+second, `"action":"complete"`)
}

func TestStarterSetupInterruptedInitRetries(t *testing.T) {
	tc, prefix := setupFixture(t)
	tc.Shell(t, `mkdir -p /tmp/failing-fest
printf '#!/bin/sh\nexit 1\n' > /tmp/failing-fest/fest
chmod +x /tmp/failing-fest/fest`)
	out, code, err := tc.ExecCommand("sh", "-c", "PATH=/tmp/failing-fest:$PATH "+prefix+"setup --json")
	require.NoError(t, err)
	require.NotZero(t, code, out)
	tc.Shell(t, "test ! -e /tmp/starter-home/camps/festival; test ! -e /tmp/starter-home/config/obey/campaign/registry.json")
	require.Equal(t, "created", setupResult(t, tc, prefix)["action"])
}

func TestStarterSetupPreservesDefaultCamp(t *testing.T) {
	tc, prefix := setupFixture(t)
	tc.Shell(t, prefix+"create default -d 'My original camp' -m 'Keep my work'")
	before, err := tc.ReadFile("/tmp/starter-home/camps/default/.campaign/campaign.yaml")
	require.NoError(t, err)
	require.Equal(t, "existing", setupResult(t, tc, prefix)["action"])
	after, err := tc.ReadFile("/tmp/starter-home/camps/default/.campaign/campaign.yaml")
	require.NoError(t, err)
	require.Equal(t, before, after)
	tc.Shell(t, "test ! -e /tmp/starter-home/camps/festival")
}

func TestStarterSetupUnwritableBaseStaysRetryable(t *testing.T) {
	tc, prefix := setupFixture(t)
	tc.Shell(t, `printf '%s' '{"campaigns_dir":"/proc/festival-starter-camps"}' > /tmp/starter-home/config/obey/campaign/config.json`)
	out, code, err := tc.ExecCommand("sh", "-c", prefix+"setup --json")
	require.NoError(t, err)
	require.NotZero(t, code, out)
	tc.Shell(t, "test ! -e /tmp/starter-home/config/obey/campaign/starter.json")
	tc.Shell(t, `printf '%s' '{"campaigns_dir":"/tmp/starter-home/camps"}' > /tmp/starter-home/config/obey/campaign/config.json`)
	require.Equal(t, "created", setupResult(t, tc, prefix)["action"])
}
