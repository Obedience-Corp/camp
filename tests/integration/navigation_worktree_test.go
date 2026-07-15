//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupWorktreeNavCampaign creates a campaign with a single submodule project
// and returns the campaign and project paths inside the container.
func setupWorktreeNavCampaign(t *testing.T, tc *TestContainer, name string) (campPath, projPath string) {
	t.Helper()
	campPath = "/campaigns/" + name
	bareDir := "/test/" + name + "-origin.git"
	seedDir := "/test/" + name + "-seed"

	tc.Shell(t, fmt.Sprintf(`
set -e
git init --bare %[1]s
git clone %[1]s %[2]s
git -C %[2]s config user.email test@test.com
git -C %[2]s config user.name Test
printf '# Test\n' > %[2]s/README.md
git -C %[2]s add . && git -C %[2]s commit -m 'init'
git -C %[2]s branch -M main
git -C %[2]s push origin main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main
`, bareDir, seedDir))

	_, err := tc.InitCampaign(campPath, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
set -e
cd %[1]s
GIT_ALLOW_PROTOCOL=file git submodule add %[2]s projects/proj
git commit -m 'add proj'
`, campPath, bareDir))

	return campPath, campPath + "/projects/proj"
}

// TestGoWorktree_ResolvesAcrossAllLocations verifies that worktree navigation
// (cgo wt <project>@) uses git as the source of truth and finds every worktree
// for a project, not only those under the conventional
// projects/worktrees/<project>/ layout. It also verifies that plain directories
// which are not registered git worktrees are excluded.
func TestGoWorktree_ResolvesAcrossAllLocations(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath := setupWorktreeNavCampaign(t, tc, "wt-nav-all-locations")

	// pref-wt lives in the preferred location: projects/worktrees/proj/pref-wt
	// loose-wt lives OUTSIDE the preferred location, a sibling directly under
	// projects/worktrees/ (mirroring a real worktree created without camp).
	// not-a-worktree is a plain directory under the preferred location that git
	// does not track as a worktree, so it must never resolve.
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s/projects/worktrees/proj/pref-wt -b pref-wt
git -C %[1]s worktree add %[2]s/projects/worktrees/loose-wt -b loose-wt
mkdir -p %[2]s/projects/worktrees/proj/not-a-worktree
`, projPath, campPath))

	// Force a fresh navigation index so the assertions do not depend on cache
	// staleness heuristics.
	_, err := tc.RunCampInDir(campPath, "cache", "rebuild")
	require.NoError(t, err)

	// The preferred-location worktree resolves.
	out, err := tc.RunCampInDir(campPath, "go", "wt", "proj@pref-wt", "--print")
	require.NoError(t, err, "output:\n%s", out)
	assert.Contains(t, out, "projects/worktrees/proj/pref-wt",
		"preferred-location worktree should resolve")

	// The non-preferred-location worktree resolves too. This is the regression:
	// before the fix it was dropped because enumeration scanned only the
	// preferred directory instead of git worktree list.
	out, err = tc.RunCampInDir(campPath, "go", "wt", "proj@loose-wt", "--print")
	require.NoError(t, err, "output:\n%s", out)
	assert.Contains(t, out, "projects/worktrees/loose-wt",
		"worktree outside the preferred location must resolve")
	assert.NotContains(t, out, "projects/worktrees/proj/loose-wt",
		"loose worktree must resolve to its real path, not the preferred layout")

	// The ambiguous query lists both worktrees and excludes the plain directory.
	out, err = tc.RunCampInDir(campPath, "go", "wt", "proj@", "--print")
	require.NoError(t, err, "output:\n%s", out)
	assert.Contains(t, out, "proj@pref-wt", "preferred worktree should be listed")
	assert.Contains(t, out, "proj@loose-wt", "non-preferred worktree should be listed")
	assert.NotContains(t, out, "not-a-worktree",
		"a directory that is not a git worktree must not be listed")
}

// TestCompleteWorktree_BranchesAcrossLocations verifies that shell completion of
// project@branch pulls from the git-backed navigation index, so worktrees are
// offered regardless of where they live on disk.
func TestCompleteWorktree_BranchesAcrossLocations(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath := setupWorktreeNavCampaign(t, tc, "wt-nav-complete")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s/projects/worktrees/proj/pref-wt -b pref-wt
git -C %[1]s worktree add %[2]s/projects/worktrees/loose-wt -b loose-wt
`, projPath, campPath))

	_, err := tc.RunCampInDir(campPath, "cache", "rebuild")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(campPath, "complete", "wt", "proj@")
	require.NoError(t, err, "output:\n%s", out)
	assert.Contains(t, out, "proj@pref-wt", "completion should offer the preferred worktree")
	assert.Contains(t, out, "proj@loose-wt",
		"completion should offer the worktree outside the preferred location")
}
