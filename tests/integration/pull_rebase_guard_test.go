//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullAllSkipsPreExistingRebase(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/campaigns/pull-rebase-guard"
	const normalDir = campaignDir + "/projects/normal-project"
	const rebaseDir = campaignDir + "/projects/rebase-project"

	setupPullRebaseGuardFixture(t, tc, campaignDir, normalDir, rebaseDir)

	output, err := tc.RunCampInDir(campaignDir, "pull", "all", "--ff-only")
	require.NoError(t, err, output)
	assert.Contains(t, output, "rebase in progress")

	exists, err := tc.CheckFileExists(normalDir + "/pulled.txt")
	require.NoError(t, err)
	assert.True(t, exists, "normal submodule should still pull while another repo is rebasing")

	rebaseGitDir := strings.TrimSpace(tc.GitOutput(t, rebaseDir, "rev-parse", "--absolute-git-dir"))
	exists, err = tc.CheckFileExists(rebaseGitDir + "/REBASE_HEAD")
	require.NoError(t, err)
	assert.True(t, exists, "pre-existing rebase metadata must not be aborted")

	status := tc.Shell(t, fmt.Sprintf("git -C %s status", rebaseDir))
	assert.Contains(t, status, "rebasing")
	assert.NotContains(t, status, "nothing to commit")

	porcelain := tc.GitOutput(t, rebaseDir, "status", "--porcelain")
	assert.Contains(t, porcelain, "UU conflict.txt")
}

func setupPullRebaseGuardFixture(t *testing.T, tc *TestContainer, campaignDir, normalDir, rebaseDir string) {
	t.Helper()

	tc.Shell(t, `
set -e
git init --bare --initial-branch=main /test/normal-origin.git
git clone /test/normal-origin.git /test/normal-seed
git -C /test/normal-seed config user.email test@test.com
git -C /test/normal-seed config user.name Test
printf '# Normal\n' > /test/normal-seed/README.md
git -C /test/normal-seed add .
git -C /test/normal-seed commit -m 'initial normal'
git -C /test/normal-seed push origin main

git init --bare --initial-branch=main /test/rebase-origin.git
git clone /test/rebase-origin.git /test/rebase-seed
git -C /test/rebase-seed config user.email test@test.com
git -C /test/rebase-seed config user.name Test
printf 'base\n' > /test/rebase-seed/conflict.txt
git -C /test/rebase-seed add .
git -C /test/rebase-seed commit -m 'initial rebase'
git -C /test/rebase-seed push origin main
`)

	_, err := tc.InitCampaign(campaignDir, "Pull Rebase Guard", "")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
set -e
cd %[1]s
git branch -M main
GIT_ALLOW_PROTOCOL=file git submodule add /test/normal-origin.git projects/normal-project
GIT_ALLOW_PROTOCOL=file git submodule add /test/rebase-origin.git projects/rebase-project
git commit -m 'add projects'
git -C %[2]s branch --set-upstream-to=origin/main main
git -C %[3]s branch --set-upstream-to=origin/main main

git clone /test/normal-origin.git /test/normal-advance
git -C /test/normal-advance config user.email test@test.com
git -C /test/normal-advance config user.name Test
printf 'pulled\n' > /test/normal-advance/pulled.txt
git -C /test/normal-advance add .
git -C /test/normal-advance commit -m 'advance normal'
git -C /test/normal-advance push origin main

git clone /test/rebase-origin.git /test/rebase-advance
git -C /test/rebase-advance config user.email test@test.com
git -C /test/rebase-advance config user.name Test
printf 'remote change\n' > /test/rebase-advance/conflict.txt
git -C /test/rebase-advance add .
git -C /test/rebase-advance commit -m 'remote conflict'
git -C /test/rebase-advance push origin main

printf 'local change\n' > %[3]s/conflict.txt
git -C %[3]s add conflict.txt
git -C %[3]s commit -m 'local conflict'
git -C %[3]s fetch origin main
if git -C %[3]s rebase origin/main; then
	echo 'expected rebase conflict'
	exit 1
fi
test -f "$(git -C %[3]s rev-parse --git-path REBASE_HEAD)"
`, campaignDir, normalDir, rebaseDir))
}
