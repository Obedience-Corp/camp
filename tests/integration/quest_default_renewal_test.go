//go:build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuestDefault_FreshCreatesOnce(t *testing.T) {
	root := "/campaigns/default-fresh-resolution"
	tc := setupQuestCreateJSONCampaign(t, root)
	tc.Shell(t, "rm -rf "+root+"/.campaign/quests")
	head := tc.GitOutput(t, root, "rev-parse", "HEAD")
	for attempt := range 2 {
		stdout, stderr, code := runQuestCreateJSON(t, tc, root, "", false, "quest", "default", "--json")
		require.Zero(t, code, stderr)
		var result struct {
			SchemaVersion string `json:"schema_version"`
			Quest         struct {
				ID string `json:"id"`
			} `json:"quest"`
			Created bool `json:"created"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &result), stdout)
		require.Equal(t, "quest-default/v1alpha1", result.SchemaVersion)
		require.Equal(t, "qst_default", result.Quest.ID)
		require.Equal(t, attempt == 0, result.Created)
	}
	require.Equal(t, head, tc.GitOutput(t, root, "rev-parse", "HEAD"), "fallback creation must not hold the command on a commit")
}

func TestQuestDefault_RenewalJSONPreservesHistory(t *testing.T) {
	for _, transition := range []string{"pause", "complete", "archive"} {
		t.Run(transition, func(t *testing.T) {
			root := "/campaigns/default-renew-" + transition
			tc := setupQuestCreateJSONCampaign(t, root)
			out, err := tc.RunCampInDir(root, "quest", transition, "qst_default", "--no-commit")
			require.NoError(t, err, out)
			old, err := tc.RunCampInDir(root, "quest", "show", "qst_default", "--json")
			require.NoError(t, err, old)
			var historical struct {
				Quest struct {
					Path string `json:"path"`
				} `json:"quest"`
			}
			require.NoError(t, json.Unmarshal([]byte(old), &historical))
			before, err := tc.ReadFile(root + "/" + historical.Quest.Path)
			require.NoError(t, err)
			var id string
			for attempt := range 2 {
				stdout, stderr, code := runQuestCreateJSON(t, tc, root, "", false, "quest", "default", "--json")
				require.Zero(t, code, stderr)
				var result struct {
					SchemaVersion string `json:"schema_version"`
					Quest         struct {
						ID      string `json:"id"`
						Status  string `json:"status"`
						Default bool   `json:"default"`
					} `json:"quest"`
					Created bool `json:"created"`
				}
				require.NoError(t, json.Unmarshal([]byte(stdout), &result), stdout)
				require.Equal(t, "quest-default/v1alpha1", result.SchemaVersion)
				require.Equal(t, "open", result.Quest.Status)
				require.True(t, result.Quest.Default)
				require.Equal(t, attempt == 0, result.Created)
				require.NotEqual(t, "qst_default", result.Quest.ID)
				if attempt == 0 {
					id = result.Quest.ID
				} else {
					require.Equal(t, id, result.Quest.ID)
				}
			}
			after, err := tc.ReadFile(root + "/" + historical.Quest.Path)
			require.NoError(t, err)
			require.Equal(t, before, after)
			listed, err := tc.RunCampInDir(root, "quest", "list", "--json", "--no-scaffold")
			require.NoError(t, err, listed)
			require.Contains(t, listed, id)
			require.Contains(t, listed, `"default": true`)
		})
	}
}
