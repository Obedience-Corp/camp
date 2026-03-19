//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installMockPlugin creates a shell script at /usr/local/bin/camp-<name> that
// echoes its arguments and environment. This lets us verify plugin dispatch
// without installing a real plugin binary.
func installMockPlugin(tc *TestContainer, name string) error {
	script := `#!/bin/sh
echo "plugin-name: ` + name + `"
echo "plugin-args: $@"
echo "plugin-camproot: $CAMP_ROOT"
`
	path := "/usr/local/bin/camp-" + name
	if err := tc.WriteFile(path, script); err != nil {
		return err
	}
	_, _, err := tc.ExecCommand("chmod", "+x", path)
	return err
}

// installMockPluginExitCode creates a mock plugin that exits with the given code.
func installMockPluginExitCode(tc *TestContainer, name string, exitCode int) error {
	script := `#!/bin/sh
echo "plugin-name: ` + name + `"
exit ` + string(rune('0'+exitCode))
	path := "/usr/local/bin/camp-" + name
	if err := tc.WriteFile(path, script); err != nil {
		return err
	}
	_, _, err := tc.ExecCommand("chmod", "+x", path)
	return err
}

func TestPlugin_DispatchBasic(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/plugin-basic"

	_, err := tc.InitCampaign(campaignPath, "plugin-basic", "product")
	require.NoError(t, err)

	err = installMockPlugin(tc, "mygraph")
	require.NoError(t, err)

	// camp mygraph → dispatches to camp-mygraph
	output, err := tc.RunCampInDir(campaignPath, "mygraph")
	require.NoError(t, err, "plugin dispatch should succeed")
	assert.Contains(t, output, "plugin-name: mygraph", "should execute the plugin")
}

func TestPlugin_ForwardsArgs(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/plugin-args"

	_, err := tc.InitCampaign(campaignPath, "plugin-args", "product")
	require.NoError(t, err)

	err = installMockPlugin(tc, "tool")
	require.NoError(t, err)

	// camp tool build --verbose → camp-tool build --verbose
	output, err := tc.RunCampInDir(campaignPath, "tool", "build", "--verbose")
	require.NoError(t, err, "plugin with args should succeed")
	assert.Contains(t, output, "plugin-args: build --verbose", "should forward all args")
}

func TestPlugin_SetsCampRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/plugin-root"

	_, err := tc.InitCampaign(campaignPath, "plugin-root", "product")
	require.NoError(t, err)

	err = installMockPlugin(tc, "envcheck")
	require.NoError(t, err)

	// Run from campaign root — CAMP_ROOT should be set
	output, err := tc.RunCampInDir(campaignPath, "envcheck")
	require.NoError(t, err, "plugin should succeed")
	assert.Contains(t, output, "plugin-camproot: "+campaignPath,
		"CAMP_ROOT should be set to campaign root")
}

func TestPlugin_SetsCampRootFromSubdir(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/plugin-subdir"

	_, err := tc.InitCampaign(campaignPath, "plugin-subdir", "product")
	require.NoError(t, err)

	// Create a subdirectory
	_, _, err = tc.ExecCommand("mkdir", "-p", campaignPath+"/docs/notes")
	require.NoError(t, err)

	err = installMockPlugin(tc, "deepcheck")
	require.NoError(t, err)

	// Run from deep inside the campaign — CAMP_ROOT should still resolve
	output, err := tc.RunCampInDir(campaignPath+"/docs/notes", "deepcheck")
	require.NoError(t, err, "plugin from subdirectory should succeed")
	assert.Contains(t, output, "plugin-camproot: "+campaignPath,
		"CAMP_ROOT should be set even from subdirectory")
}

func TestPlugin_BuiltinCommandsTakePriority(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/plugin-priority"

	_, err := tc.InitCampaign(campaignPath, "plugin-priority", "product")
	require.NoError(t, err)

	// Install a plugin named camp-project — should be shadowed by builtin
	err = installMockPlugin(tc, "project")
	require.NoError(t, err)

	// camp project list → should use builtin, not plugin
	output, err := tc.RunCampInDir(campaignPath, "project", "list")
	require.NoError(t, err, "builtin project command should work")
	// The output should NOT contain our plugin marker
	assert.NotContains(t, output, "plugin-name: project",
		"builtin commands must take priority over plugins with the same name")
}

func TestPlugin_NotFound_FallsThrough(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/plugin-notfound"

	_, err := tc.InitCampaign(campaignPath, "plugin-notfound", "product")
	require.NoError(t, err)

	// camp nonexistent → no camp-nonexistent on PATH, Cobra handles error
	_, err = tc.RunCampInDir(campaignPath, "nonexistent")
	require.Error(t, err, "unknown command with no plugin should fail")
}

func TestPlugin_NonZeroExitPropagated(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/plugin-exit"

	_, err := tc.InitCampaign(campaignPath, "plugin-exit", "product")
	require.NoError(t, err)

	err = installMockPluginExitCode(tc, "failing", 1)
	require.NoError(t, err)

	// camp failing → camp-failing exits 1, camp should propagate the error
	_, err = tc.RunCampInDir(campaignPath, "failing")
	require.Error(t, err, "non-zero exit from plugin should propagate as error")
}

func TestPlugin_ListCommand(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/plugin-list"

	_, err := tc.InitCampaign(campaignPath, "plugin-list", "product")
	require.NoError(t, err)

	err = installMockPlugin(tc, "alpha")
	require.NoError(t, err)
	err = installMockPlugin(tc, "beta")
	require.NoError(t, err)

	// camp plugins → should list both
	output, err := tc.RunCampInDir(campaignPath, "plugins")
	require.NoError(t, err, "camp plugins should succeed")
	assert.Contains(t, output, "alpha", "should list alpha plugin")
	assert.Contains(t, output, "beta", "should list beta plugin")
}

func TestPlugin_NoArgs_ShowsHelp(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/plugin-noargs"

	_, err := tc.InitCampaign(campaignPath, "plugin-noargs", "product")
	require.NoError(t, err)

	err = installMockPlugin(tc, "shouldnotrun")
	require.NoError(t, err)

	// camp (no args) → should show help, not dispatch any plugin
	output, err := tc.RunCampInDir(campaignPath)
	require.NoError(t, err, "camp with no args should show help")
	assert.NotContains(t, output, "plugin-name:",
		"no plugin should be dispatched when camp is called with no args")
	assert.Contains(t, output, "camp", "should show help output")
}
