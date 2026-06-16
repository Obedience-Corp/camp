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

func setupSyncFallbackCampaign(t *testing.T, tc *TestContainer, name string) (campPath, projPath, barePath string) {
	t.Helper()
	campPath = "/campaigns/" + name
	barePath = "/test/" + name + "-origin.git"
	seedDir := "/test/" + name + "-seed"

	tc.Shell(t, fmt.Sprintf(`
set -e
git init --bare %[1]s
git clone %[1]s %[2]s
git -C %[2]s config user.email test@test.com
git -C %[2]s config user.name Test
printf '# Proj\n' > %[2]s/README.md
git -C %[2]s add . && git -C %[2]s commit -m 'init'
git -C %[2]s branch -M main
git -C %[2]s push origin main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main
`, barePath, seedDir))

	_, err := tc.InitCampaign(campPath, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
set -e
cd %[1]s
GIT_ALLOW_PROTOCOL=file git submodule add %[2]s projects/subproj
git -C %[1]s commit -m 'add subproj'
`, campPath, barePath))

	projPath = campPath + "/projects/subproj"
	return campPath, projPath, barePath
}

func TestSyncFallback_QuarantineDirty(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupRemoveSubmoduleCampaign(t, tc, "sync-fallback")

	// Setup hostile: make the superproject record an unreachable submodule SHA
	// while the local submodule worktree has dirty content.
	tc.Shell(t, fmt.Sprintf(`
set -e
fake_sha=1111111111111111111111111111111111111111
git -C %[2]s update-index --cacheinfo 160000 "$fake_sha" projects/subproj
git -C %[2]s commit -m 'record unreachable submodule pointer'
printf 'dirty work\n' > %[1]s/dirty.txt
	`, projPath, campPath))

	// Trigger fallback path. The command exits non-zero because the restored
	// default-branch checkout intentionally differs from the unreachable gitlink.
	output, err := tc.RunCampInDir(campPath, "sync", "--force")
	require.Error(t, err, "sync should report drift after stale fallback")
	assert.Contains(t, output, "recorded commit unavailable")

	// Assert: dirty file was preserved in the quarantine sibling.
	quarantine := strings.TrimSpace(tc.Shell(t, fmt.Sprintf(`find %[1]s/projects -maxdepth 1 -type d -name 'subproj.sync-quarantine-*' | head -n 1`, campPath)))
	require.NotEmpty(t, quarantine, "dirty submodule dir should be quarantine-renamed")
	exists, err := tc.CheckFileExists(quarantine + "/dirty.txt")
	require.NoError(t, err)
	assert.True(t, exists, "dirty file must survive in quarantine")

	// Assert: .git points at parent-managed submodule metadata, not a standalone clone.
	tc.Shell(t, fmt.Sprintf(`
set -e
test -e %[1]s/.git
if [ ! -L %[1]s/.git ]; then
	grep -q '^gitdir: ' %[1]s/.git
fi
`, projPath))

	// Assert: submodule status ok
	status := tc.Shell(t, fmt.Sprintf(`git -C %s submodule status projects/subproj || echo status-failed`, campPath))
	assert.NotContains(t, status, "fatal", "submodule status should not be fatal")
}
