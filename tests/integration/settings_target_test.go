//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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

func TestIntegration_SettingsExplicitCampaignRejectsWithoutWrites(t *testing.T) {
	tc := GetSharedContainer(t)
	const a = "/test/settings-readonly-a"
	const b = "/test/settings-readonly-b"
	for _, name := range []string{"settings-readonly-a", "settings-readonly-b"} {
		out, err := tc.RunCamp("init", "/test/"+name, "--name", name, "-d", "Read-only validation test", "-m", "original", "--no-git", "--force")
		require.NoError(t, err, out)
	}
	const manifestPath = b + "/.campaign/campaign.yaml"
	original, err := tc.ReadFile(manifestPath)
	require.NoError(t, err)
	// Hash every camp configuration file to catch rewrites and newly created
	// files, including jumps.yaml and local settings, on either failure path.
	snapshot := func(root string) string {
		return tc.Shell(t, "cd "+root+" && find .campaign -type f -exec sha256sum {} \\; | sort")
	}
	for _, defect := range []string{"missing-id", "mismatched-id", "missing-manifest"} {
		for _, verb := range []string{"get", "set"} {
			for _, cwd := range []string{a, b} {
				t.Run(defect+"/"+verb+"/"+strings.TrimPrefix(cwd, "/test/"), func(t *testing.T) {
					var manifest map[string]any
					require.NoError(t, yaml.Unmarshal([]byte(original), &manifest))
					if defect == "missing-id" {
						delete(manifest, "id")
					} else if defect == "mismatched-id" {
						manifest["id"] = "00000000-0000-4000-8000-000000000001"
					}
					data, err := yaml.Marshal(manifest)
					require.NoError(t, err)
					require.NoError(t, tc.WriteFile(manifestPath, string(data)))
					tc.Shell(t, "rm -f "+b+"/.campaign/settings/jumps.yaml")
					if defect == "missing-manifest" {
						tc.Shell(t, "rm "+manifestPath)
					}
					beforeA, beforeB := snapshot(a), snapshot(b)
					args := []string{"settings", verb, "local.campaign.mission"}
					if verb == "set" {
						args = append(args, "must-not-write")
					}
					args = append(args, "--campaign", "settings-readonly-b")
					out, err := tc.RunCampInDir(cwd, args...)
					require.Error(t, err, out)
					if defect == "missing-manifest" {
						require.Contains(t, out, "load target camp")
					} else {
						require.Contains(t, out, "identity no longer matches")
					}
					require.Equal(t, beforeA, snapshot(a), "changed the other camp on rejection")
					require.Equal(t, beforeB, snapshot(b), "changed the selected camp on rejection")
				})
			}
		}
	}
}

func TestIntegration_SettingsCampaignScalarReadAndRejectedSetDoNotWrite(t *testing.T) {
	tc := GetSharedContainer(t)
	const a = "/test/settings-scalar-readonly-a"
	const b = "/test/settings-scalar-readonly-b"
	for _, name := range []string{"settings-scalar-readonly-a", "settings-scalar-readonly-b"} {
		out, err := tc.RunCamp("init", "/test/"+name, "--name", name, "-d", "Scalar read-only test", "-m", "original", "--no-git", "--force")
		require.NoError(t, err, out)
	}
	tc.Shell(t, "rm -f "+a+"/.campaign/settings/jumps.yaml "+b+"/.campaign/settings/jumps.yaml")
	beforeA, err := tc.ReadFile(a + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	beforeB, err := tc.ReadFile(b + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	assertUnchanged := func() {
		t.Helper()
		afterA, err := tc.ReadFile(a + "/.campaign/campaign.yaml")
		require.NoError(t, err)
		require.Equal(t, beforeA, afterA, "changed the current camp")
		afterB, err := tc.ReadFile(b + "/.campaign/campaign.yaml")
		require.NoError(t, err)
		require.Equal(t, beforeB, afterB, "changed the selected camp")
		tc.Shell(t, "test ! -e "+a+"/.campaign/settings/jumps.yaml && test ! -e "+b+"/.campaign/settings/jumps.yaml")
	}

	for _, args := range [][]string{
		{"settings", "get", "local.campaign.mission", "--campaign", "settings-scalar-readonly-b"},
		{"settings", "set", "local.campaign.type", "invalid", "--campaign", "settings-scalar-readonly-b"},
	} {
		out, err := tc.RunCampInDir(a, args...)
		if args[1] == "get" {
			require.NoError(t, err, out)
			require.Equal(t, "original", strings.TrimSpace(out))
		} else {
			require.Error(t, err, out)
			require.Contains(t, out, "unknown camp type")
		}
		assertUnchanged()
	}

	// The same read path is used when no explicit camp is selected.
	out, err := tc.RunCampInDir(b, "settings", "get", "local.campaign.mission")
	require.NoError(t, err, out)
	require.Equal(t, "original", strings.TrimSpace(out))
	assertUnchanged()

	out, err = tc.RunCampInDir(a, "settings", "set", "local.campaign.mission", "updated", "--campaign", "settings-scalar-readonly-b")
	require.NoError(t, err, out)
	afterB, err := tc.ReadFile(b + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	require.NotEqual(t, beforeB, afterB, "accepted set did not save the selected camp")
	out, err = tc.RunCampInDir(a, "settings", "get", "local.campaign.mission", "--campaign", "settings-scalar-readonly-b")
	require.NoError(t, err, out)
	require.Equal(t, "updated", strings.TrimSpace(out))
	tc.Shell(t, "test ! -e "+a+"/.campaign/settings/jumps.yaml && test ! -e "+b+"/.campaign/settings/jumps.yaml")
}
