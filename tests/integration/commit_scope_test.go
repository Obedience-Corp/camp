//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntegration_CommitRefusesPreStagedSubmoduleRef(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/commit-prestaged-ref"
	setupRefsSyncCampaignWithDrift(t, tc, campaignDir, "alpha")
	before := gitCommitCount(t, tc, campaignDir)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'root content' > root.txt
		git add projects/alpha
	`, campaignDir))

	output, err := tc.RunCampInDir(campaignDir, "commit", "-m", "root content")
	require.Error(t, err, "camp commit should refuse pre-staged submodule refs")
	require.Contains(t, output, "staged submodule ref(s)")
	require.Contains(t, output, "projects/alpha")
	require.Contains(t, output, "camp refs-sync")
	require.Contains(t, output, "--include-refs")

	after := gitCommitCount(t, tc, campaignDir)
	require.Equal(t, before, after, "refusal must not create a campaign root commit")

	staged := strings.Fields(tc.GitOutput(t, campaignDir, "diff", "--cached", "--name-only"))
	require.Contains(t, staged, "projects/alpha", "pre-staged ref should remain staged for explicit recovery")
	require.Contains(t, staged, "root.txt", "content staged by the attempted commit should remain staged")
}

func TestIntegration_CommitRefsOnlyDriftPrintsHint(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/commit-refs-only"
	setupRefsSyncCampaignWithDrift(t, tc, campaignDir, "alpha")
	before := gitCommitCount(t, tc, campaignDir)

	output, err := tc.RunCampInDir(campaignDir, "commit", "-m", "refs only")
	require.NoError(t, err, "refs-only drift should be a friendly no-op; output:\n%s", output)
	require.Contains(t, output, "submodule ref changes are excluded by default")
	require.Contains(t, output, "camp refs-sync")
	require.NotContains(t, output, "no changes added to commit")

	after := gitCommitCount(t, tc, campaignDir)
	require.Equal(t, before, after, "refs-only drift should not create a campaign root commit")

	staged := strings.TrimSpace(tc.GitOutput(t, campaignDir, "diff", "--cached", "--name-only"))
	require.Empty(t, staged, "refs-only drift should leave the campaign root index empty")
}
