//go:build integration
// +build integration

package integration

import (
	"fmt"
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

	// Setup hostile: commit in sub, push, then make super record an old SHA (stale).
	// Add dirty file in sub.
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s checkout -b feature
printf 'new\n' > %[1]s/new.txt
git -C %[1]s add new.txt && git -C %[1]s commit -m 'new'
GIT_ALLOW_PROTOCOL=file git -C %[1]s push origin feature
git -C %[1]s checkout main
# Record an old SHA in super (simulate stale recorded)
git -C %[1]s update-ref refs/heads/feature $(git -C %[1]s rev-parse HEAD~1) || true
printf 'dirty work\n' > %[1]s/dirty.txt
`, projPath))

	// Trigger fallback path by running a command that causes sub init/update with stale.
	// Use sync --force which may hit the graceful.
	_, _ = tc.RunCampInDir(campPath, "sync", "--force")

	// Assert: dirty file still exists (no RemoveAll destroyed it)
	exists, err := tc.CheckFileExists(projPath + "/dirty.txt")
	require.NoError(t, err)
	assert.True(t, exists, "dirty file must survive (quarantine or no removal)")

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
