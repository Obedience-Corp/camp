//go:build integration
// +build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var gitHookLocalEnvNames = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_PREFIX",
}

func TestPrePushHook_ClearsGitHookEnvironment(t *testing.T) {
	tc := GetSharedContainer(t)

	installPrePushHookFixture(t, tc)
	installJustGateRecorder(t, tc)

	output, exitCode, err := tc.ExecCommand("sh", "-lc", prePushHookScript())
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "pre-push hook failed:\n%s", output)

	args, err := tc.ReadFile("/test/gate-args.txt")
	require.NoError(t, err)
	require.Equal(t, "gate-push\n", args)

	gateEnv := readEnvFile(t, tc, "/test/gate-env.txt")
	for _, name := range gitHookLocalEnvNames {
		value, ok := gateEnv[name]
		require.False(t, ok, "%s leaked into gate command with value %q", name, value)
	}
}

func installPrePushHookFixture(t *testing.T, tc *TestContainer) {
	t.Helper()

	hook, err := os.ReadFile(filepath.Join(campSourceRoot(t), ".githooks", "pre-push"))
	require.NoError(t, err)

	tc.Shell(t, "mkdir -p /test/camp-source/.githooks && git init /test/camp-source")
	require.NoError(t, tc.WriteFile("/test/camp-source/.githooks/pre-push", string(hook)))
	tc.Shell(t, "chmod +x /test/camp-source/.githooks/pre-push")
}

func installJustGateRecorder(t *testing.T, tc *TestContainer) {
	t.Helper()

	tc.Shell(t, "mkdir -p /test/bin")
	require.NoError(t, tc.WriteFile("/test/bin/just", `#!/usr/bin/env sh
set -eu
printf '%s\n' "$*" > /test/gate-args.txt
env > /test/gate-env.txt
`))
	tc.Shell(t, "chmod +x /test/bin/just")
}

func prePushHookScript() string {
	const zeroSHA = "0000000000000000000000000000000000000000"
	return `
set -eu
git init /test/hook-env-repo >/dev/null
cd /test/camp-source
PATH=/test/bin:$PATH \
GIT_DIR=/test/hook-env-repo/.git \
GIT_WORK_TREE=/test/hook-env-repo \
GIT_INDEX_FILE=/test/hook-env-index \
GIT_PREFIX=hooks/ \
CAMP_GATE_FAST= \
CAMP_GATE_FULL= \
sh .githooks/pre-push <<'PRE_PUSH_STDIN'
refs/heads/main ` + zeroSHA + ` refs/heads/main ` + zeroSHA + `
PRE_PUSH_STDIN
`
}

func readEnvFile(t *testing.T, tc *TestContainer, path string) map[string]string {
	t.Helper()

	data, err := tc.ReadFile(path)
	require.NoError(t, err)

	env := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		name, value, ok := strings.Cut(line, "=")
		if ok {
			env[name] = value
		}
	}
	return env
}

func campSourceRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	require.NoError(t, err)

	root, err := filepath.Abs(filepath.Join(wd, "..", ".."))
	require.NoError(t, err)
	return root
}
