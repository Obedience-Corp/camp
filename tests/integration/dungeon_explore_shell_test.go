//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDungeonExploreShellEndOfOptions(t *testing.T) {
	exploreShellHop(t, "explore-shell-end", "camp dungeon explore --")
}

// A root flag is valid before or between the command words. The wrapper
// found `dungeon explore` only as the first two words, so these opened the
// feed without the handoff file and g left the shell where it was.
func TestDungeonExploreShellRootFlagBeforeCommand(t *testing.T) {
	for _, tc := range []struct{ name, campaign, line string }{
		{"flag first", "explore-shell-flag-first", "camp --no-color dungeon explore"},
		{"flag between the words", "explore-shell-flag-between", "camp dungeon --no-color=false explore"},
	} {
		t.Run(tc.name, func(t *testing.T) { exploreShellHop(t, tc.campaign, tc.line) })
	}
}

// exploreShellHop runs line in bash, zsh, and fish with shell integration
// loaded, presses g on the one finished item, and requires the shell to end
// up in that item's directory.
func exploreShellHop(t *testing.T, campaign, line string) {
	t.Helper()
	tc := GetSharedContainer(t)
	root := setupDungeonCampaign(t, tc, campaign)
	require.NoError(t, tc.WriteFile(root+"/dungeon/completed/finished.md", "# Finished fixture\n"))
	installShells(t, tc)
	tc.Shell(t, "mkdir -p /explore-bin && ln -sf /camp /explore-bin/camp")
	for _, sh := range []string{"bash", "zsh", "fish"} {
		t.Run(sh, func(t *testing.T) {
			require.NoError(t, tc.WriteFile("/tmp/explore-shell-init", shellInitScript(t, tc, sh)))
			script := "export PATH=/explore-bin:$PATH; source /tmp/explore-shell-init; " + line + "; pwd > /tmp/explore-shell-cwd"
			if sh == "fish" {
				script = "set -gx PATH /explore-bin $PATH; source /tmp/explore-shell-init; " + line + "; pwd > /tmp/explore-shell-cwd"
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
