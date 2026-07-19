//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_WorkitemTags(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/test/workitem-tags"
	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Workitem Tags Test",
		"--type", "product",
		"-d", "Tags integration",
		"-m", "Verify --tag normalization and dedupe",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	t.Run("CreateNormalizesAndDedupesTags", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "tagged-item",
			"--type", "feature", "--title", "Tagged", "--tag", "A", "--tag", "a")
		require.NoError(t, err, "create --tag: %s", out)

		marker, err := tc.ReadFile(campaignDir + "/workflow/feature/tagged-item/.workitem")
		require.NoError(t, err)
		assert.Contains(t, marker, "tags:")
		assert.Contains(t, marker, "- a")
		assert.NotContains(t, marker, "- A", "tags must normalize to lowercase")
		assert.Equal(t, 1, strings.Count(marker, "- a"), "duplicate tags must collapse to one entry")
	})

	t.Run("AdoptNormalizesTags", func(t *testing.T) {
		_, _, err := tc.ExecCommand("mkdir", "-p", campaignDir+"/workflow/feature/adopt-tagged")
		require.NoError(t, err)

		out, err := tc.RunCampInDir(campaignDir, "workitem", "adopt", "workflow/feature/adopt-tagged",
			"--type", "feature", "--title", "Adopted", "--tag", "Foo", "--tag", "foo")
		require.NoError(t, err, "adopt --tag: %s", out)

		marker, err := tc.ReadFile(campaignDir + "/workflow/feature/adopt-tagged/.workitem")
		require.NoError(t, err)
		assert.Contains(t, marker, "tags:")
		assert.Contains(t, marker, "- foo")
		assert.NotContains(t, marker, "- Foo", "tags must normalize to lowercase")
		assert.Equal(t, 1, strings.Count(marker, "- foo"), "duplicate tags must collapse to one entry")
	})
}
