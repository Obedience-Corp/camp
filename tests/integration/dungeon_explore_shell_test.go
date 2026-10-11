//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDungeonExploreShellEndOfOptions(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupDungeonCampaign(t, tc, "explore-shell-end")
	require.NoError(t, tc.WriteFile(root+"/dungeon/completed/finished.md", "# Finished fixture\n"))
	installShells(t, tc)
	tc.Shell(t, "mkdir -p /explore-bin && ln -s /camp /explore-bin/camp")
	for _, sh := range []string{"bash", "zsh", "fish"} {
		t.Run(sh, func(t *testing.T) {
			require.NoError(t, tc.WriteFile("/tmp/explore-shell-init", shellInitScript(t, tc, sh)))
			script := "export PATH=/explore-bin:$PATH; source /tmp/explore-shell-init; camp dungeon explore --; pwd > /tmp/explore-shell-cwd"
			if sh == "fish" {
				script = "set -gx PATH /explore-bin $PATH; source /tmp/explore-shell-init; camp dungeon explore --; pwd > /tmp/explore-shell-cwd"
			}
			require.NoError(t, tc.WriteFile("/tmp/explore-shell-script", script))
			output, err := tc.runCampInteractive(root, nil, 30*time.Second, []InteractiveStep{{WaitFor: "finished.md", Input: "g"}}, "run", sh, "/tmp/explore-shell-script")
			require.NoError(t, err, "%s", output)
			selected, err := tc.ReadFile("/tmp/explore-shell-cwd")
			require.NoError(t, err)
			require.Equal(t, root+"/dungeon/completed", strings.TrimSpace(selected))
		})
	}
}
