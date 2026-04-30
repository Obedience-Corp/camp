//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupLeverageCampaign initializes a campaign with two small Go projects
// committed to git so `camp leverage` has something to score. Returns the
// campaign root path inside the container.
//
// scc and git history (at least one commit) are required by the leverage
// pipeline — without commits, ResolveProjects/CountAuthors return empty
// results and the table output skips the project rows we want to assert on.
func setupLeverageCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()

	root := "/campaigns/" + name
	_, err := tc.InitCampaign(root, name, "")
	require.NoError(t, err, "camp init should succeed")

	// Create two project subdirs with a tiny Go file each, committed via git.
	// Project layout matches what camp's project resolver expects (a git repo
	// per project under projects/<name>/). Without InitGitRepo + a commit the
	// leverage author resolver finds 0 authors and skips the project.
	for _, project := range []string{"alpha", "beta"} {
		projectPath := root + "/projects/" + project
		tc.Shell(t, fmt.Sprintf(`
			set -e
			mkdir -p %s
			cat > %s/main.go <<'EOF'
package main

func main() {
	println("hello from %s")
}
EOF
			cd %s
			git init -q
			git add main.go
			git -c user.email=test@test.com -c user.name=Test commit -q -m "initial"
		`, projectPath, projectPath, project, projectPath))
	}

	return root
}

// TestLeverage_TableOutput exercises the default table output of `camp leverage`.
//
// Migrated from cmd/camp/leverage/main_command_test.go::TestLeverageCommand_TableOutput
// because the original test invoked the live cobra RunE against the host's
// campaign root (via campaign.DetectCached), persisting snapshots to
// .campaign/leverage/snapshots/ on every run. The host-fs mutation made
// `just test unit` constantly dirty the campaign repo.
func TestLeverage_TableOutput(t *testing.T) {
	if !sccAvailable {
		t.Fatal("scc binary failed to build at container init time. " +
			"These tests gate the leverage integration coverage and must not " +
			"silently skip on upstream scc breakage; investigate the build " +
			"error logged by NewSharedContainer and either fix or roll back to " +
			"a known-good scc release.")
	}

	tc := GetSharedContainer(t)
	root := setupLeverageCampaign(t, tc, "leverage-table")

	output, err := tc.RunCampInDir(root, "leverage")
	require.NoError(t, err, "camp leverage should succeed: %s", output)

	for _, want := range []string{
		"Campaign Leverage:",
		"COCOMO Estimate:",
		"person-months",
		"Actual Effort:",
		"Team Equivalent:",
		"PROJECT",
		"FILES",
		"CODE",
		"AUTHORS",
		"EST COST",
		"EST PM",
		"ACTUAL PM",
		"LEVERAGE",
	} {
		assert.Contains(t, output, want, "table output missing %q", want)
	}
}

// TestLeverage_JSONOutput exercises the `--json` flag.
//
// Migrated from cmd/camp/leverage/main_command_test.go::TestLeverageCommand_JSONOutput.
func TestLeverage_JSONOutput(t *testing.T) {
	if !sccAvailable {
		t.Fatal("scc binary failed to build at container init time. " +
			"These tests gate the leverage integration coverage and must not " +
			"silently skip on upstream scc breakage; investigate the build " +
			"error logged by NewSharedContainer and either fix or roll back to " +
			"a known-good scc release.")
	}

	tc := GetSharedContainer(t)
	root := setupLeverageCampaign(t, tc, "leverage-json")

	output, err := tc.RunCampInDir(root, "leverage", "--json")
	require.NoError(t, err, "camp leverage --json should succeed: %s", output)

	// Output may have a leading `Created leverage config at ...` notice on the
	// first run before the JSON. Strip everything before the first `{`.
	jsonStart := strings.Index(output, "{")
	require.GreaterOrEqual(t, jsonStart, 0, "no JSON object in output: %s", output)

	var result struct {
		Campaign map[string]any   `json:"campaign"`
		Projects []map[string]any `json:"projects"`
	}
	require.NoError(t, json.Unmarshal([]byte(output[jsonStart:]), &result), "parse JSON: %s", output)
	require.NotNil(t, result.Campaign, "campaign score is nil in JSON")
	assert.NotEmpty(t, result.Projects, "no projects in JSON output")
}

// TestLeverage_ProjectFilter exercises `--project` valid and invalid filters.
//
// Migrated from cmd/camp/leverage/main_command_test.go::TestLeverageCommand_ProjectFilter.
func TestLeverage_ProjectFilter(t *testing.T) {
	if !sccAvailable {
		t.Fatal("scc binary failed to build at container init time. " +
			"These tests gate the leverage integration coverage and must not " +
			"silently skip on upstream scc breakage; investigate the build " +
			"error logged by NewSharedContainer and either fix or roll back to " +
			"a known-good scc release.")
	}

	tc := GetSharedContainer(t)
	root := setupLeverageCampaign(t, tc, "leverage-filter")

	t.Run("valid project filter", func(t *testing.T) {
		output, err := tc.RunCampInDir(root, "leverage", "--project", "alpha")
		require.NoError(t, err, "camp leverage --project alpha should succeed: %s", output)
		assert.Contains(t, output, "alpha", "output should contain filtered project 'alpha'")
	})

	t.Run("invalid project filter returns error", func(t *testing.T) {
		output, err := tc.RunCampInDir(root, "leverage", "--project", "nonexistent")
		require.Error(t, err, "camp leverage --project nonexistent should fail")
		assert.Contains(t, output, "project not found", "error should mention project not found")
	})
}

// TestLeverage_RunnerError verifies that `camp leverage` warns and continues
// when the scc binary fails per-project (rather than hard-failing the whole
// command).
//
// Migrated from cmd/camp/leverage/main_command_test.go::TestLeverageCommand_RunnerError.
// The original mocked the Runner interface with an error from Run(). Here we
// shadow the real scc binary with a stub that exits non-zero, so
// `initRunner` (which exec.LookPath("scc")) succeeds but every per-project
// Run() invocation fails — exercising the same `Warning: skipping <project>`
// loop branch.
func TestLeverage_RunnerError(t *testing.T) {
	if !sccAvailable {
		t.Fatal("scc binary failed to build at container init time. " +
			"These tests gate the leverage integration coverage and must not " +
			"silently skip on upstream scc breakage; investigate the build " +
			"error logged by NewSharedContainer and either fix or roll back to " +
			"a known-good scc release.")
	}

	tc := GetSharedContainer(t)
	root := setupLeverageCampaign(t, tc, "leverage-runner-err")

	// Replace the real scc binary with a failing stub at the same path so
	// exec.LookPath("scc") still finds something but every Run() invocation
	// fails. Put the stub at /usr/local/bin/scc (where the real one lives) —
	// in alpine /usr/local/bin precedes /usr/bin in PATH, so a stub elsewhere
	// would be shadowed. Stash the real binary aside and restore on cleanup.
	tc.Shell(t, `set -e
mv /usr/local/bin/scc /usr/local/bin/scc.real
cat > /usr/local/bin/scc <<'EOF'
#!/bin/sh
echo "stub scc: simulated failure" >&2
exit 1
EOF
chmod +x /usr/local/bin/scc`)
	t.Cleanup(func() {
		_, _, _ = tc.ExecCommand("sh", "-c", "rm -f /usr/local/bin/scc && mv /usr/local/bin/scc.real /usr/local/bin/scc")
	})

	output, err := tc.RunCampInDir(root, "leverage")
	require.NoError(t, err, "camp leverage should not hard-fail on per-project scc errors: %s", output)
	assert.Contains(t, output, "Warning", "output should contain a Warning line for the failing scc run")
}

// TestLeverage_ConfigDisplay exercises `camp leverage config` with no flags.
//
// Migrated from cmd/camp/leverage/main_command_test.go::TestLeverageConfigCommand_Display.
// The original test read the host's real campaign config; here we read a
// fresh ephemeral campaign created inside the container.
func TestLeverage_ConfigDisplay(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupLeverageCampaign(t, tc, "leverage-config")

	output, err := tc.RunCampInDir(root, "leverage", "config")
	require.NoError(t, err, "camp leverage config should succeed: %s", output)

	for _, want := range []string{"Team Size:", "COCOMO Type:", "Config path:"} {
		assert.Contains(t, output, want, "config output missing %q", want)
	}
}
