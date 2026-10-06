//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSwitch_AtTargetLandsInProject drives compiled `camp switch camp@target`
// against a registered camp in the container: the part after @ resolves like
// `camp go` inside that camp, with projects winning over same-named targets
// elsewhere, and completion offers projects next to the navigation tabs.
func TestSwitch_AtTargetLandsInProject(t *testing.T) {
	tc := GetSharedContainer(t)

	const camp = "switch-at-target"
	root := "/campaigns/" + camp
	_, err := tc.RunCamp("create", camp,
		"-d", "switch at target", "-m", "land in a project", "--no-git", "--path", "/campaigns")
	require.NoError(t, err)

	// A project plus a same-named festival decoy: the project must win.
	tc.Shell(t, `
set -e
mkdir -p `+root+`/projects/keepshot
mkdir -p `+root+`/festivals/planning/keepshot
`)

	t.Run("project name lands in projects/<name>", func(t *testing.T) {
		output, err := tc.RunCamp("switch", camp+"@keepshot", "--print")
		require.NoError(t, err, "output: %s", output)
		assert.Equal(t, root+"/projects/keepshot", strings.TrimSpace(output))
	})

	t.Run("tab drill form lands inside the tab", func(t *testing.T) {
		output, err := tc.RunCamp("switch", camp+"@p@keepshot", "--print")
		require.NoError(t, err, "output: %s", output)
		assert.Equal(t, root+"/projects/keepshot", strings.TrimSpace(output))
	})

	t.Run("plain tab still lands on the tab directory", func(t *testing.T) {
		output, err := tc.RunCamp("switch", camp+"@p", "--print")
		require.NoError(t, err, "output: %s", output)
		assert.Equal(t, root+"/projects", strings.TrimSpace(output))
	})

	t.Run("unknown target reports not found", func(t *testing.T) {
		output, err := tc.RunCamp("switch", camp+"@zzz-no-such-target", "--print")
		require.Error(t, err)
		assert.Contains(t, output, `"zzz-no-such-target" not found in camp `+camp)
	})

	t.Run("completion offers tabs and projects", func(t *testing.T) {
		output, err := tc.RunCamp("__complete", "switch", camp+"@")
		require.NoError(t, err, "output: %s", output)
		assert.Contains(t, output, camp+"@p\n")
		assert.Contains(t, output, camp+"@keepshot\n")
	})

	t.Run("completion narrows to the project prefix", func(t *testing.T) {
		output, err := tc.RunCamp("__complete", "switch", camp+"@keep")
		require.NoError(t, err, "output: %s", output)
		assert.Contains(t, output, camp+"@keepshot\n")
		assert.NotContains(t, output, camp+"@p\n")
	})
}

// TestSwitch_AtTargetRefusesRemovedWorktree warms the nav index with a linked
// worktree that lives outside the camp, removes it with git, and checks that
// `camp switch camp@project@branch` refuses the stale cached destination
// instead of printing a path that no longer exists.
func TestSwitch_AtTargetRefusesRemovedWorktree(t *testing.T) {
	tc := GetSharedContainer(t)

	const camp = "switch-at-stale"
	root := "/campaigns/" + camp
	_, err := tc.RunCamp("create", camp,
		"-d", "switch at stale", "-m", "refuse removed worktree", "--no-git", "--path", "/campaigns")
	require.NoError(t, err)

	require.NoError(t, tc.CreateGitRepo(root+"/projects/proj"))
	tc.Shell(t, `
set -e
git -C `+root+`/projects/proj worktree add /tmp/`+camp+`-feature -b feature
`)

	output, err := tc.RunCamp("switch", camp+"@proj@feature", "--print")
	require.NoError(t, err, "output: %s", output)
	require.Equal(t, "/tmp/"+camp+"-feature", strings.TrimSpace(output), "index must discover the external worktree")

	tc.Shell(t, `git -C `+root+`/projects/proj worktree remove --force /tmp/`+camp+`-feature`)

	output, err = tc.RunCamp("switch", camp+"@proj@feature", "--print")
	require.Error(t, err, "a removed worktree must not resolve; output: %s", output)
	assert.Contains(t, output, "no longer exists")
	assert.NotEqual(t, "/tmp/"+camp+"-feature", strings.TrimSpace(output), "stale path must not be reported as the destination")

	output, err = tc.RunCamp("switch", camp+"@proj", "--print")
	require.NoError(t, err, "the project itself must still resolve; output: %s", output)
	assert.Equal(t, root+"/projects/proj", strings.TrimSpace(output))
}
