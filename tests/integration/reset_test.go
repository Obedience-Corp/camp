//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReset_ClearsPooledCheckoutArtifacts is the isolation contract for
// TestFresh_DoesNotDeleteRemoteBranches and
// TestIntentPromote_MigratesLegacyIntentMarkerToCanonicalRoot: those tests
// flake when a previous checkout leaves a fixture under /test, /campaigns,
// /root/.obey, or /tmp/create-* and Reset reports success anyway.
func TestReset_ClearsPooledCheckoutArtifacts(t *testing.T) {
	tc := GetSharedContainer(t)

	tc.Shell(t, `
set -e
mkdir -p /test/fresh-remote-branch-origin.git \
  /campaigns/promote-marker-migration/workflow/intents \
  /root/.obey/campaign \
  /tmp/create-leak
printf 'leftover origin\n' > /test/fresh-remote-branch-origin.git/HEAD
printf '# leaked marker\n' > /campaigns/promote-marker-migration/workflow/intents/OBEY.md
printf '{"leaked": true}\n' > /root/.obey/campaign/registry.json
printf 'create leak\n' > /tmp/create-leak/file
printf 'frame leak\n' > /tmp/.camp_frame_leak
`)

	require.NoError(t, tc.Reset(), "Reset after planting shared-path leftovers")

	type leftover struct {
		path   string
		isDir  bool
		reason string
	}
	leftovers := []leftover{
		{"/test/fresh-remote-branch-origin.git", true, "fresh remote-branch fixture"},
		{"/campaigns/promote-marker-migration", true, "promote marker-migration fixture"},
		{"/root/.obey/campaign/registry.json", false, "leaked registry"},
		{"/tmp/create-leak", true, "create-test tmp root"},
		{"/tmp/.camp_frame_leak", false, "session frame capture"},
	}
	for _, item := range leftovers {
		var (
			exists bool
			err    error
		)
		if item.isDir {
			exists, err = tc.CheckDirExists(item.path)
		} else {
			exists, err = tc.CheckFileExists(item.path)
		}
		require.NoError(t, err, "stat %s", item.path)
		if exists {
			t.Fatalf("Reset left %s (%s)\n%s", item.path, item.reason, tc.SharedStateDump())
		}
	}

	testRoot, err := tc.CheckDirExists("/test")
	require.NoError(t, err)
	require.True(t, testRoot, "Reset should recreate /test")

	campaignsRoot, err := tc.CheckDirExists("/campaigns")
	require.NoError(t, err)
	require.True(t, campaignsRoot, "Reset should recreate /campaigns")

	cfg, err := tc.ReadFile("/root/.obey/campaign/config.json")
	require.NoError(t, err)
	require.Equal(t, `{"dungeon_hidden": false}`, cfg, "Reset should reseed the suite global config")
}
