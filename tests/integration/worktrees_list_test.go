//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorktreesList_ResolvesAcrossAllLocations verifies that `camp worktrees
// list --json` and `camp project worktree list` use git as the source of
// truth for enumeration, so they report every worktree for a project, not
// only those under the conventional projects/worktrees/<project>/ layout.
// This is the same regression PR #429 fixed for `cgo wt` navigation; see
// TestGoWorktree_ResolvesAcrossAllLocations in navigation_worktree_test.go.
func TestWorktreesList_ResolvesAcrossAllLocations(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath := setupWorktreeNavCampaign(t, tc, "wt-list-all-locations")

	// pref-wt lives in the preferred location: projects/worktrees/proj/pref-wt
	// loose-wt lives OUTSIDE the preferred location, a sibling directly under
	// projects/worktrees/ (mirroring a real worktree created without camp).
	// not-a-worktree is a plain directory under the preferred location that
	// git does not track as a worktree, so it must never be listed.
	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s/projects/worktrees/proj/pref-wt -b pref-wt
git -C %[1]s worktree add %[2]s/projects/worktrees/loose-wt -b loose-wt
mkdir -p %[2]s/projects/worktrees/proj/not-a-worktree
`, projPath, campPath))

	out, err := tc.RunCampInDir(campPath, "worktrees", "list", "--json")
	require.NoError(t, err, "output:\n%s", out)

	var result struct {
		Worktrees []struct {
			Project string `json:"project"`
			Name    string `json:"name"`
			Path    string `json:"path"`
			Branch  string `json:"branch"`
		} `json:"worktrees"`
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result), "parse JSON: %s", out)

	pathByName := make(map[string]string, len(result.Worktrees))
	for _, wt := range result.Worktrees {
		pathByName[wt.Name] = wt.Path
	}

	prefPath, hasPref := pathByName["pref-wt"]
	require.True(t, hasPref, "preferred-location worktree missing from JSON output: %+v", result.Worktrees)
	assert.Contains(t, prefPath, "projects/worktrees/proj/pref-wt")

	loosePath, hasLoose := pathByName["loose-wt"]
	require.True(t, hasLoose, "worktree outside the preferred location must appear in camp worktrees list --json")
	assert.Contains(t, loosePath, "projects/worktrees/loose-wt")
	assert.NotContains(t, loosePath, "projects/worktrees/proj/loose-wt",
		"loose worktree must report its real path, not the preferred layout")

	_, hasCruft := pathByName["not-a-worktree"]
	assert.False(t, hasCruft, "a directory that is not a git worktree must not be listed")

	// camp project worktree list agrees: same worktrees, real paths.
	out, err = tc.RunCampInDir(campPath, "project", "worktree", "list", "--project", "proj")
	require.NoError(t, err, "output:\n%s", out)
	assert.Contains(t, out, "pref-wt")
	assert.Contains(t, out, "loose-wt")
	assert.Contains(t, out, "projects/worktrees/loose-wt")
	assert.NotContains(t, out, "projects/worktrees/proj/loose-wt")
	assert.NotContains(t, out, "not-a-worktree")
}

// TestWorktreesList_DisambiguatesSameBasename verifies that two linked
// worktrees for the same project whose directory basenames match (a preferred
// projects/worktrees/proj/dup and a loose projects/worktrees/dup) do not
// collapse to one "dup" name in --json: each keeps a unique, path-derived name.
func TestWorktreesList_DisambiguatesSameBasename(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath := setupWorktreeNavCampaign(t, tc, "wt-list-dup-basename")

	tc.Shell(t, fmt.Sprintf(`
set -e
git -C %[1]s worktree add %[2]s/projects/worktrees/proj/dup -b dup-pref
git -C %[1]s worktree add %[2]s/projects/worktrees/dup -b dup-loose
`, projPath, campPath))

	out, err := tc.RunCampInDir(campPath, "worktrees", "list", "--json")
	require.NoError(t, err, "output:\n%s", out)

	var result struct {
		Worktrees []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"worktrees"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result), "parse JSON: %s", out)

	nameByPathSuffix := map[string]string{}
	names := map[string]int{}
	for _, wt := range result.Worktrees {
		names[wt.Name]++
		switch {
		case strings.HasSuffix(wt.Path, "/projects/worktrees/proj/dup"):
			nameByPathSuffix["proj/dup"] = wt.Name
		case strings.HasSuffix(wt.Path, "/projects/worktrees/dup"):
			nameByPathSuffix["dup"] = wt.Name
		}
	}

	prefName, hasPref := nameByPathSuffix["proj/dup"]
	looseName, hasLoose := nameByPathSuffix["dup"]
	require.True(t, hasPref, "preferred dup worktree missing: %s", out)
	require.True(t, hasLoose, "loose dup worktree missing: %s", out)
	assert.NotEqual(t, prefName, looseName, "same-basename worktrees must get distinct names")
	assert.Equal(t, 1, names[prefName], "disambiguated name %q must be unique in output", prefName)
	assert.Equal(t, 1, names[looseName], "disambiguated name %q must be unique in output", looseName)
	assert.Contains(t, prefName, "projects/worktrees/proj/dup")
	assert.Contains(t, looseName, "projects/worktrees/dup")
}

// TestWorktreesList_ScopesToProjectContextAndSupportsFilter verifies that the
// list command detects both a project's main checkout and one of its linked
// worktrees, while retaining an explicit project filter from the campaign
// root.
func TestWorktreesList_ScopesToProjectContextAndSupportsFilter(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath := setupWorktreeNavCampaign(t, tc, "wt-list-project-context")
	otherBare := "/test/wt-list-project-context-other-origin.git"
	otherSeed := "/test/wt-list-project-context-other-seed"
	otherPath := campPath + "/projects/other"

	tc.Shell(t, fmt.Sprintf(`
set -e
git init --bare %[1]s
git clone %[1]s %[2]s
git -C %[2]s config user.email test@test.com
git -C %[2]s config user.name Test
printf '# Other\n' > %[2]s/README.md
git -C %[2]s add . && git -C %[2]s commit -m 'init other'
git -C %[2]s branch -M main
git -C %[2]s push origin main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main
cd %[3]s
GIT_ALLOW_PROTOCOL=file git submodule add %[1]s projects/other
git commit -m 'add other'
git -C %[4]s worktree add %[3]s/projects/worktrees/proj/proj-context -b proj-context
git -C %[5]s worktree add %[3]s/projects/worktrees/other/other-context -b other-context
`, otherBare, otherSeed, campPath, projPath, otherPath))

	// Outside a project, the command retains the campaign-wide view.
	out, err := tc.RunCampInDir(campPath, "worktrees", "list")
	require.NoError(t, err, "campaign-wide list: %s", out)
	assert.Contains(t, out, "proj-context")
	assert.Contains(t, out, "other-context")

	// From the project's main checkout, only that project's worktrees appear.
	out, err = tc.RunCampInDir(projPath, "worktrees", "list")
	require.NoError(t, err, "project-context list: %s", out)
	assert.Contains(t, out, "proj-context")
	assert.NotContains(t, out, "other-context")

	// A linked worktree is also a project context, even though it is not under
	// the registered projects/<name> checkout path.
	worktreePath := campPath + "/projects/worktrees/proj/proj-context"
	out, err = tc.RunCampInDir(worktreePath, "worktrees", "list")
	require.NoError(t, err, "worktree-context list: %s", out)
	assert.Contains(t, out, "proj-context")
	assert.NotContains(t, out, "other-context")

	// From the campaign root, --project remains the explicit filter.
	out, err = tc.RunCampInDir(campPath, "worktrees", "list", "--project", "other")
	require.NoError(t, err, "filtered list: %s", out)
	assert.Contains(t, out, "other-context")
	assert.NotContains(t, out, "proj-context")
}

// TestWorktreesList_ScopesViaSymlinkedProjectPath proves path normalization
// keeps project detection stable when the project checkout is reached through
// a symlink (common on macOS). Filesystem mutation stays in the container
// harness per repo test policy — not host TempDir/Symlink unit tests.
func TestWorktreesList_ScopesViaSymlinkedProjectPath(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath := setupWorktreeNavCampaign(t, tc, "wt-list-symlink-scope")
	otherBare := "/test/wt-list-symlink-scope-other-origin.git"
	otherSeed := "/test/wt-list-symlink-scope-other-seed"
	otherPath := campPath + "/projects/other"
	projLink := "/test/wt-list-symlink-scope-proj-link"

	tc.Shell(t, fmt.Sprintf(`
set -e
git init --bare %[1]s
git clone %[1]s %[2]s
git -C %[2]s config user.email test@test.com
git -C %[2]s config user.name Test
printf '# Other\n' > %[2]s/README.md
git -C %[2]s add . && git -C %[2]s commit -m 'init other'
git -C %[2]s branch -M main
git -C %[2]s push origin main
git --git-dir %[1]s symbolic-ref HEAD refs/heads/main
cd %[3]s
GIT_ALLOW_PROTOCOL=file git submodule add %[1]s projects/other
git commit -m 'add other'
git -C %[4]s worktree add %[3]s/projects/worktrees/proj/proj-sym -b proj-sym
git -C %[5]s worktree add %[3]s/projects/worktrees/other/other-sym -b other-sym
ln -sfn %[4]s %[6]s
`, otherBare, otherSeed, campPath, projPath, otherPath, projLink))

	// cwd is the symlink form; registered project path is the real checkout.
	// normalizePath/pathWithin must still scope to proj only.
	out, err := tc.RunCampInDir(projLink, "worktrees", "list")
	require.NoError(t, err, "symlink project-context list: %s", out)
	assert.Contains(t, out, "proj-sym")
	assert.NotContains(t, out, "other-sym")
}
