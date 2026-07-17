//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyCampaign creates a campaign scaffolded with the visible dungeon
// layout, i.e. one made before hidden dungeons became the default.
func legacyCampaign(t *testing.T, tc *TestContainer, path, name string, extraArgs ...string) {
	t.Helper()
	require.NoError(t, tc.WriteGlobalConfig(`{"dungeon_hidden": false}`))
	args := append([]string{"init", path, "--name", name, "-d", "d", "-m", "m"}, extraArgs...)
	out, err := tc.RunCamp(args...)
	require.NoError(t, err, "camp init: %s", out)

	exists, err := tc.CheckDirExists(path + "/dungeon")
	require.NoError(t, err)
	require.True(t, exists, "setup: expected a legacy visible dungeon")
}

// commitScaffold commits the freshly scaffolded campaign inside the container,
// giving the repository the history a real legacy campaign would have. camp
// init git-inits but does not commit, and the commit path builds its index
// from HEAD.
func commitScaffold(t *testing.T, tc *TestContainer, path string) {
	t.Helper()
	tc.Shell(t, "cd "+path+" && git add -A && git -c user.email=t@t -c user.name=t "+
		"commit -q -m scaffold")
}

// TestDungeonMigrate_ConvertsEveryDiscoveredDungeon is the core acceptance
// case. A campaign carries roughly a dozen dungeons, so migration sweeps every
// one it discovers on disk rather than a hardcoded list, and lands them in a
// single commit.
func TestDungeonMigrate_ConvertsEveryDiscoveredDungeon(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/migrate-all"
	legacyCampaign(t, tc, path, "migrate-all", "--no-git")

	// Locations camp init does not scaffold, plus a dungeon nested inside an
	// archived work item, which is the shape a depth-limited sweep would miss.
	extra := []string{
		path + "/festivals/dungeon/completed",
		path + "/.campaign/quests/dungeon/archived",
		path + "/workflow/bugs/dungeon/someday",
		path + "/workflow/pitch/dungeon/completed",
		path + "/workflow/design/dungeon/completed/go-token-counter/dungeon/someday",
	}
	for _, dir := range extra {
		_, exitCode, err := tc.ExecCommand("mkdir", "-p", dir)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode)
	}
	require.NoError(t, tc.WriteFile(path+"/dungeon/completed/keep-me.md", "# payload\n"))
	require.NoError(t, tc.WriteFile(path+"/workflow/design/dungeon/completed/go-token-counter/dungeon/someday/nested.md", "# nested payload\n"))

	out, err := tc.RunCampInDir(path, "dungeon", "migrate")
	require.NoError(t, err, "camp dungeon migrate: %s", out)

	for _, dir := range []string{
		path + "/.dungeon",
		path + "/festivals/.dungeon",
		path + "/.campaign/intents/.dungeon",
		path + "/.campaign/quests/.dungeon",
		path + "/workflow/design/.dungeon",
		path + "/workflow/explore/.dungeon",
		path + "/workflow/reviews/.dungeon",
		path + "/workflow/bugs/.dungeon",
		path + "/workflow/pitch/.dungeon",
		path + "/workflow/design/.dungeon/completed/go-token-counter/.dungeon",
	} {
		exists, err := tc.CheckDirExists(dir)
		require.NoError(t, err)
		assert.True(t, exists, "expected migrated hidden dungeon at %s", dir)
	}

	for _, dir := range []string{
		path + "/dungeon",
		path + "/festivals/dungeon",
		path + "/.campaign/intents/dungeon",
		path + "/workflow/design/dungeon",
	} {
		exists, err := tc.CheckDirExists(dir)
		require.NoError(t, err)
		assert.False(t, exists, "visible dungeon should be gone at %s", dir)
	}

	// Contents survive the move, including inside the nested dungeon.
	content, err := tc.ReadFile(path + "/.dungeon/completed/keep-me.md")
	require.NoError(t, err)
	assert.Contains(t, content, "payload")
	nested, err := tc.ReadFile(path + "/workflow/design/.dungeon/completed/go-token-counter/.dungeon/someday/nested.md")
	require.NoError(t, err)
	assert.Contains(t, nested, "nested payload")

	// A migrated campaign is fully migrated: re-running is a clean no-op.
	again, err := tc.RunCampInDir(path, "dungeon", "migrate")
	require.NoError(t, err, "re-running migrate should be a no-op: %s", again)
	assert.Contains(t, again, "Nothing to migrate")
}

// TestDungeonMigrate_SkipsProjects is the guard against corrupting real
// repositories. projects/ holds project checkouts, and a source directory
// named "dungeon" inside one (camp itself has internal/dungeon and
// cmd/camp/dungeon) is a Go package, not a campaign dungeon.
func TestDungeonMigrate_SkipsProjects(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/migrate-skips-projects"
	legacyCampaign(t, tc, path, "migrate-skips-projects", "--no-git")

	projectDungeons := []string{
		path + "/projects/camp/internal/dungeon",
		path + "/projects/camp/cmd/camp/dungeon",
		path + "/projects/fest/methodology/festivals/dungeon",
	}
	for _, dir := range projectDungeons {
		_, exitCode, err := tc.ExecCommand("mkdir", "-p", dir)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode)
	}
	require.NoError(t, tc.WriteFile(path+"/projects/camp/internal/dungeon/resolver.go", "package dungeon\n"))

	out, err := tc.RunCampInDir(path, "dungeon", "migrate")
	require.NoError(t, err, "camp dungeon migrate: %s", out)

	assert.NotContains(t, out, "projects/", "migration output must never name a path under projects/")

	for _, dir := range projectDungeons {
		exists, err := tc.CheckDirExists(dir)
		require.NoError(t, err)
		assert.True(t, exists, "project directory %s must be untouched", dir)
	}
	for _, dir := range []string{
		path + "/projects/camp/internal/.dungeon",
		path + "/projects/camp/cmd/camp/.dungeon",
		path + "/projects/fest/methodology/festivals/.dungeon",
	} {
		exists, err := tc.CheckDirExists(dir)
		require.NoError(t, err)
		assert.False(t, exists, "migration must not create %s inside a project", dir)
	}

	content, err := tc.ReadFile(path + "/projects/camp/internal/dungeon/resolver.go")
	require.NoError(t, err)
	assert.Contains(t, content, "package dungeon", "project source must be untouched")

	// The campaign's own dungeon still migrated.
	exists, err := tc.CheckDirExists(path + "/.dungeon")
	require.NoError(t, err)
	assert.True(t, exists, "the campaign's own dungeon should still have migrated")
}

// TestDungeonMigrate_DryRunMutatesNothing verifies the preview is a preview.
func TestDungeonMigrate_DryRunMutatesNothing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/migrate-dry-run"
	legacyCampaign(t, tc, path, "migrate-dry-run", "--no-git")

	out, err := tc.RunCampInDir(path, "dungeon", "migrate", "--dry-run")
	require.NoError(t, err, "camp dungeon migrate --dry-run: %s", out)
	assert.Contains(t, out, "Dry run")
	assert.Contains(t, out, "dungeon -> .dungeon")

	for _, dir := range []string{
		path + "/dungeon",
		path + "/.campaign/intents/dungeon",
		path + "/workflow/design/dungeon",
	} {
		exists, err := tc.CheckDirExists(dir)
		require.NoError(t, err)
		assert.True(t, exists, "--dry-run must leave %s in place", dir)
	}
	for _, dir := range []string{
		path + "/.dungeon",
		path + "/.campaign/intents/.dungeon",
		path + "/workflow/design/.dungeon",
	} {
		exists, err := tc.CheckDirExists(dir)
		require.NoError(t, err)
		assert.False(t, exists, "--dry-run must not create %s", dir)
	}
}

// TestDungeonMigrate_RefusesOnConflict proves the all-or-nothing rule: a
// location holding both spellings needs a human to decide what to keep, and no
// other dungeon may move in the meantime.
func TestDungeonMigrate_RefusesOnConflict(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/migrate-conflict"
	legacyCampaign(t, tc, path, "migrate-conflict", "--no-git")

	_, exitCode, err := tc.ExecCommand("mkdir", "-p", path+"/workflow/design/.dungeon")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(path, "dungeon", "migrate")
	require.NoError(t, err)
	require.NotEqual(t, 0, exitCode, "migrate must refuse a conflicted campaign (stdout=%s)", stdout)
	assert.Contains(t, stdout+stderr, "workflow/design", "the refusal should name the conflicting location")

	// No partial work: every other dungeon is untouched.
	for _, dir := range []string{
		path + "/dungeon",
		path + "/.campaign/intents/dungeon",
		path + "/workflow/explore/dungeon",
	} {
		exists, err := tc.CheckDirExists(dir)
		require.NoError(t, err)
		assert.True(t, exists, "a refused migration must not move anything; %s moved", dir)
	}
	exists, err := tc.CheckDirExists(path + "/.dungeon")
	require.NoError(t, err)
	assert.False(t, exists, "a refused migration must not create any hidden dungeon")
}

// TestDungeonMigrate_CommitsAsRenames checks the git half: one commit, and the
// moves recorded as renames so history survives.
func TestDungeonMigrate_CommitsAsRenames(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/migrate-git"
	legacyCampaign(t, tc, path, "migrate-git")

	commitScaffold(t, tc, path)
	before := tc.GitOutput(t, path, "rev-parse", "HEAD")

	out, err := tc.RunCampInDir(path, "dungeon", "migrate")
	require.NoError(t, err, "camp dungeon migrate: %s", out)

	after := tc.GitOutput(t, path, "rev-parse", "HEAD")
	assert.NotEqual(t, before, after, "migrate should have committed")

	count := tc.GitOutput(t, path, "rev-list", "--count", before+"..HEAD")
	assert.Equal(t, "1", count, "migration should land as exactly one commit")

	subject := tc.GitOutput(t, path, "log", "-1", "--pretty=%s")
	assert.Contains(t, subject, "Migrate", "commit subject should name the action")

	// Rename detection: the tree moved rather than being rewritten.
	names := tc.GitOutput(t, path, "show", "--name-status", "--find-renames", "-M", "--pretty=", "HEAD")
	assert.Contains(t, names, "R", "moves should be recorded as renames, got:\n"+names)
	assert.Contains(t, names, ".dungeon/")

	status := tc.GitOutput(t, path, "status", "--porcelain")
	assert.NotContains(t, status, "dungeon", "migration should leave no stray dungeon changes behind:\n"+status)

	exists, err := tc.CheckDirExists(path + "/.dungeon")
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestDungeonMigrate_CommitsNestedDungeons combines the two halves the other
// git test and the sweep test each cover alone: a dungeon nested inside
// another dungeon, in a repository with history. The nested dungeon is renamed
// first and then has its ancestor renamed out from under it, so it never
// arrives at the path the plan recorded for it. Staging that path fails.
func TestDungeonMigrate_CommitsNestedDungeons(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/migrate-nested-git"
	legacyCampaign(t, tc, path, "migrate-nested-git")

	nested := path + "/workflow/design/dungeon/completed/go-token-counter/dungeon/someday"
	_, exitCode, err := tc.ExecCommand("mkdir", "-p", nested)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	require.NoError(t, tc.WriteFile(nested+"/nested.md", "# nested payload\n"))
	commitScaffold(t, tc, path)
	before := tc.GitOutput(t, path, "rev-parse", "HEAD")

	out, err := tc.RunCampInDir(path, "dungeon", "migrate")
	require.NoError(t, err, "migrate must commit a campaign with nested dungeons: %s", out)

	count := tc.GitOutput(t, path, "rev-list", "--count", before+"..HEAD")
	assert.Equal(t, "1", count, "migration should land as exactly one commit")

	// The nested dungeon comes to rest under its migrated ancestor.
	content, err := tc.ReadFile(path + "/workflow/design/.dungeon/completed/go-token-counter/.dungeon/someday/nested.md")
	require.NoError(t, err)
	assert.Contains(t, content, "nested payload")

	// The commit carries the nested rename, and nothing is left behind.
	tracked := tc.GitOutput(t, path, "ls-files", "--", "workflow/design/.dungeon")
	assert.Contains(t, tracked, "go-token-counter/.dungeon/someday/nested.md",
		"the nested file should be tracked at its final path")
	status := tc.GitOutput(t, path, "status", "--porcelain")
	assert.NotContains(t, status, "dungeon", "migration should leave no stray dungeon changes:\n"+status)
}

// TestDungeonMigrate_NoCommitLeavesChangesStaged covers the escape hatch for
// users who want to inspect or fold the migration into their own commit.
func TestDungeonMigrate_NoCommitLeavesChangesStaged(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/migrate-no-commit"
	legacyCampaign(t, tc, path, "migrate-no-commit")

	commitScaffold(t, tc, path)
	before := tc.GitOutput(t, path, "rev-parse", "HEAD")

	out, err := tc.RunCampInDir(path, "dungeon", "migrate", "--no-commit")
	require.NoError(t, err, "camp dungeon migrate --no-commit: %s", out)
	assert.Contains(t, out, "Skipped the commit")

	after := tc.GitOutput(t, path, "rev-parse", "HEAD")
	assert.Equal(t, before, after, "--no-commit must not create a commit")

	exists, err := tc.CheckDirExists(path + "/.dungeon")
	require.NoError(t, err)
	assert.True(t, exists, "--no-commit still performs the move")

	staged := tc.GitOutput(t, path, "diff", "--cached", "--name-only")
	assert.True(t, strings.Contains(staged, ".dungeon"), "renames should be left staged, got:\n"+staged)
}

// TestDungeonMigrate_UncommittedScaffoldSkipsCommit covers a campaign that was
// git-init'd but never committed. There is no HEAD to build the commit index
// from, so the moves land in the working tree and migrate says so, rather than
// reporting failure after the directories have already moved.
func TestDungeonMigrate_UncommittedScaffoldSkipsCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/migrate-unborn-head"
	legacyCampaign(t, tc, path, "migrate-unborn-head")

	out, err := tc.RunCampInDir(path, "dungeon", "migrate")
	require.NoError(t, err, "migrate must not fail on a campaign with no commits: %s", out)
	assert.Contains(t, out, "no commits yet")

	exists, err := tc.CheckDirExists(path + "/.dungeon")
	require.NoError(t, err)
	assert.True(t, exists, "the moves should still have happened")
	exists, err = tc.CheckDirExists(path + "/dungeon")
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestDungeonMigrate_StatusSurfacesLegacyNotice checks that migration is
// discoverable: a command users already run points at it.
func TestDungeonMigrate_StatusSurfacesLegacyNotice(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/migrate-notice"
	legacyCampaign(t, tc, path, "migrate-notice")
	commitScaffold(t, tc, path)

	_, stderr, exitCode, err := tc.RunCampSplitInDir(path, "status", "--short")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "status should still succeed: %s", stderr)
	assert.Contains(t, stderr, "camp dungeon migrate", "status should point a legacy campaign at the migration")

	out, err := tc.RunCampInDir(path, "dungeon", "migrate")
	require.NoError(t, err, "camp dungeon migrate: %s", out)

	// The notice clears once there is nothing to migrate.
	_, stderr, exitCode, err = tc.RunCampSplitInDir(path, "status", "--short")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.NotContains(t, stderr, "camp dungeon migrate", "a migrated campaign should not be nagged")
}
