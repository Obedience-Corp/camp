//go:build integration

package integration

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestIntegration_SettingsExplicitCampaign(t *testing.T) {
	tc := GetSharedContainer(t)
	const a = "/test/settings-target-a"
	const b = "/test/settings-target-b"
	for _, name := range []string{"settings-target-a", "settings-target-b"} {
		out, err := tc.RunCamp("init", "/test/"+name, "--name", name, "-d", "Target isolation test", "-m", "original", "--no-git", "--force")
		require.NoError(t, err, out)
	}
	beforeA, err := tc.ReadFile(a + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	out, err := tc.RunCampInDir(a, "settings", "set", "local.campaign.mission", "targeted", "--campaign", "settings-target-b")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir("/tmp", "settings", "get", "local.campaign.mission", "-c", "settings-target-b", "--json")
	require.NoError(t, err, out)
	var payload struct {
		Value string `json:"value"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &payload))
	require.Equal(t, "targeted", payload.Value)
	out, err = tc.RunCampInDir(a, "settings", "set", "local.theme_override", "dark", "-c", "settings-target-b")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir(a, "settings", "get", "local.theme_override", "-c", "settings-target-b")
	require.NoError(t, err, out)
	require.Equal(t, "dark", strings.TrimSpace(out))
	out, err = tc.RunCampInDir(a, "settings", "set", "local.commit.disable_commit_tags", "true", "-c", "settings-target-b")
	require.NoError(t, err, out)
	out, err = tc.RunCampInDir(a, "settings", "get", "effective.commit.disable_commit_tags", "-c", "settings-target-b")
	require.NoError(t, err, out)
	require.Equal(t, "true", strings.TrimSpace(out))
	beforeB, err := tc.ReadFile(b + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	for _, target := range []string{"settings-target", "missing", ""} {
		_, err = tc.RunCampInDir(a, "settings", "set", "local.campaign.mission", "wrong", "--campaign="+target)
		require.Error(t, err)
	}
	afterA, err := tc.ReadFile(a + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	require.Equal(t, beforeA, afterA)
	afterB, err := tc.ReadFile(b + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	require.Equal(t, beforeB, afterB)
}
