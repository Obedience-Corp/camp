//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntegration_RefsSyncAtomic(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/refs-sync-atomic"
	setupRefsSyncCampaignWithDrift(t, tc, campaignDir, "alpha", "beta")

	before := gitCommitCount(t, tc, campaignDir)
	output, err := tc.RunCampInDir(campaignDir, "refs-sync")
	require.NoError(t, err, "refs-sync should succeed; output:\n%s", output)

	after := gitCommitCount(t, tc, campaignDir)
	require.Equal(t, before+1, after, "refs-sync should create exactly one campaign root commit")

	subject := tc.GitOutput(t, campaignDir, "log", "-1", "--pretty=%s")
	require.Contains(t, subject, "alpha")
	require.Contains(t, subject, "beta")

	status := tc.GitOutput(t, campaignDir, "status", "--porcelain")
	require.Empty(t, status, "refs-sync should leave campaign root clean")
}

func TestIntegration_RefsSyncSafetyCheck(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/refs-sync-safety"
	setupRefsSyncCampaignWithDrift(t, tc, campaignDir, "alpha")

	before := gitCommitCount(t, tc, campaignDir)
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'staged' > staged.txt
		git add staged.txt
	`, campaignDir))

	output, err := tc.RunCampInDir(campaignDir, "refs-sync")
	require.Error(t, err, "refs-sync should refuse staged campaign-root changes")
	require.Contains(t, output, "staged changes")

	after := gitCommitCount(t, tc, campaignDir)
	require.Equal(t, before, after, "refs-sync must not commit when safety check fails")
}

func setupRefsSyncCampaignWithDrift(t *testing.T, tc *TestContainer, campaignDir string, names ...string) {
	t.Helper()

	_, err := tc.InitCampaign(campaignDir, "Refs Sync", "product")
	require.NoError(t, err)

	for _, name := range names {
		repo := "/test/refs-" + name
		require.NoError(t, tc.CreateGitRepo(repo))
		tc.Shell(t, fmt.Sprintf(`
			cd %[1]s
			git -c protocol.file.allow=always submodule add %[2]s projects/%[3]s
		`, campaignDir, repo, name))
	}

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		git commit -m 'add submodules'
	`, campaignDir))

	for _, name := range names {
		tc.Shell(t, fmt.Sprintf(`
			cd %[1]s/projects/%[2]s
			printf 'advance %[2]s' > advance.txt
			git add advance.txt
			git commit -m 'advance %[2]s'
		`, campaignDir, name))
	}
}

func gitCommitCount(t *testing.T, tc *TestContainer, repo string) int {
	t.Helper()

	out := tc.GitOutput(t, repo, "rev-list", "--count", "HEAD")
	count, err := strconv.Atoi(out)
	require.NoError(t, err)
	return count
}
