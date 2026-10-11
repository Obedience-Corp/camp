//go:build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Exercise command startup, discovery, metadata reads, filtering, and JSON
// together. A parser-only test cannot catch whitespace lost by the file reader.
func TestDungeonExploreMetadataAndEmptyStatuses(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupDungeonCampaign(t, tc, "explore-metadata-contract")
	tc.Shell(t, "mkdir -p "+root+"/dungeon/reviewed "+root+"/dungeon/completed "+root+"/dungeon/all")
	for _, status := range []string{"reviewed", "completed/", "all/"} {
		output, err := tc.RunCampInDir(root, "dungeon", "explore", "--json", "--status", status)
		require.NoError(t, err, "%s", output)
		var feed struct {
			Items []json.RawMessage `json:"items"`
		}
		require.NoError(t, json.Unmarshal([]byte(output), &feed))
		require.Empty(t, feed.Items)
	}
	require.NoError(t, tc.WriteFile(root+"/dungeon/completed/merged/.workitem", "<<: {title: Merged title, ref: WI-merged}\n"))
	require.NoError(t, tc.WriteFile(root+"/dungeon/completed/indented/.workitem", "  title: Indented title\n  ref: WI-indented\n"))
	for _, id := range []string{"WI-merged", "WI-indented"} {
		output, err := tc.RunCampInDir(root, "dungeon", "explore", "--json", "--query", id)
		require.NoError(t, err, "%s", output)
		var feed struct {
			Items []struct {
				ID      string `json:"id"`
				Warning string `json:"warning"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal([]byte(output), &feed))
		require.Len(t, feed.Items, 1)
		require.Equal(t, id, feed.Items[0].ID)
		require.Empty(t, feed.Items[0].Warning)
	}
}
