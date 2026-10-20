//go:build integration
// +build integration

package integration

import (
	"fmt"
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
exit ` + fmt.Sprintf("%d", exitCode)
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

// TestPlugin_GroupSIGINTPreservesPluginExit is the real-dispatcher Ctrl+C
// contract: SIGINT to camp's process group cancels NotifyContext, plugin
// Execute interrupts (not Kill-first), plugin finalizes exit 6, camp
// propagates 6 — not 255 from default CommandContext Kill.
func TestPlugin_GroupSIGINTPreservesPluginExit(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/plugin-group-sigint"
	readyPath := "/tmp/plugin-sigint-ready"
	finalPath := "/tmp/plugin-sigint-final"

	_, err := tc.InitCampaign(campaignPath, "plugin-group-sigint", "product")
	require.NoError(t, err)

	plugin := `#!/bin/sh
set -eu
trap 'printf done > ` + finalPath + `; exit 6' INT
printf ready > ` + readyPath + `
while :; do sleep 0.05; done
`
	require.NoError(t, tc.WriteFile("/usr/local/bin/camp-sigintprobe", plugin))
	_, _, err = tc.ExecCommand("chmod", "+x", "/usr/local/bin/camp-sigintprobe")
	require.NoError(t, err)

	// One docker-exec shell owns the lifecycle. setsid puts camp in its own
	// session/group (terminal Ctrl+C model); the waiter stays outside that group.
	// Results go to a file: session exec can drop mid-script stdout on signal paths.
	// Group delivery is mandatory: no child-only INT fallback (that would not be Ctrl+C).
	runner := `
ready=` + readyPath + `
final=` + finalPath + `
result=/tmp/plugin-sigint.result
rm -f "$ready" "$final" "$result" /tmp/plugin-sigint.out
cleanup() {
  kill -KILL -"$camp_pid" 2>/dev/null || kill -KILL "$camp_pid" 2>/dev/null || true
}
cd ` + campaignPath + ` || { echo "CD_FAIL" > "$result"; exit 1; }
setsid /camp sigintprobe >/tmp/plugin-sigint.out 2>&1 &
camp_pid=$!
i=0
while [ ! -f "$ready" ]; do
  i=$((i+1))
  if [ "$i" -gt 160 ]; then
    echo "READY_TIMEOUT" > "$result"
    cleanup
    exit 99
  fi
  sleep 0.05
done
# Process-group existence is required for terminal Ctrl+C semantics.
if ! kill -0 -"$camp_pid" 2>/dev/null; then
  echo "GROUP_MISSING" > "$result"
  cleanup
  exit 98
fi
# Fail closed if group SIGINT cannot be delivered. Do not fall back to pid-only INT.
if ! kill -INT -"$camp_pid"; then
  echo "GROUP_INT_FAIL" > "$result"
  cleanup
  exit 97
fi
# Bound the wait so a wedged camp cannot hang the suite; force-kill on timeout.
i=0
while kill -0 "$camp_pid" 2>/dev/null; do
  i=$((i+1))
  if [ "$i" -gt 100 ]; then
    echo "WAIT_TIMEOUT" > "$result"
    cleanup
    exit 96
  fi
  sleep 0.05
done
# wait's non-zero status is the plugin/camp exit we must observe; do not use set -e.
if wait "$camp_pid"; then
  code=0
else
  code=$?
fi
{
  echo "GROUP_INT=ok"
  echo "EXIT=$code"
  if [ -f "$final" ]; then echo "FINAL=yes"; else echo "FINAL=no"; fi
} > "$result"
# Surface the observed exit on the runner shell so ExecCommand exit is checkable.
exit "$code"
`
	_, code, err := tc.ExecCommand("sh", "-c", runner)
	out, _, catErr := tc.ExecCommand("cat", "/tmp/plugin-sigint.result")
	require.NoError(t, catErr, "result file missing; runner_exit=%d runner_err=%v", code, err)
	assert.Contains(t, out, "GROUP_INT=ok",
		"group SIGINT delivery must succeed (no child-only fallback); got %s (runner_exit=%d)", out, code)
	assert.Contains(t, out, "EXIT=6",
		"group SIGINT must preserve plugin exit 6, not Kill/-1→255; got %s (runner_exit=%d)", out, code)
	assert.Contains(t, out, "FINAL=yes",
		"plugin must run INT trap finalize before exit; got %s", out)
	assert.Equal(t, 6, code,
		"runner shell exit must match result EXIT=6; result=%s err=%v", out, err)
	assert.NotContains(t, out, "READY_TIMEOUT")
	assert.NotContains(t, out, "GROUP_MISSING")
	assert.NotContains(t, out, "GROUP_INT_FAIL")
	assert.NotContains(t, out, "WAIT_TIMEOUT")
}
