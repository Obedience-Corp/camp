//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the filesystem-backed commit-pref resolution paths
// (SaveGlobalConfig / SaveLocalSettings / EffectiveCommitPrefs) through the real
// `camp` binary inside the container harness, per the repo's filesystem-safety
// contract. Pure CommitPrefs / MergeCommitPrefs logic stays in the host unit
// test at internal/config/commit_prefs_test.go.

func initCommitPrefsCampaign(t *testing.T, tc *TestContainer, dir string) {
	t.Helper()
	_, err := tc.RunCamp(
		"init", dir,
		"--name", "Commit Prefs",
		"--type", "product",
		"-d", "Commit prefs integration",
		"-m", "Verify commit pref resolution",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init")
}

// TestIntegration_CommitPrefs_GlobalOnly is the container twin of the former
// host TestEffectiveCommitPrefs_GlobalOnly: global commit prefs are written and,
// with no campaign-local override, the effective values equal the global ones.
func TestIntegration_CommitPrefs_GlobalOnly(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commit-prefs-global"
	initCommitPrefsCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "settings", "set", "global.commit.sync_project_refs", "true")
	require.NoError(t, err, "set global sync")
	_, err = tc.RunCampInDir(dir, "settings", "set", "global.commit.disable_commit_tags", "true")
	require.NoError(t, err, "set global disable-tags")

	syncOut, err := tc.RunCampInDir(dir, "settings", "get", "effective.commit.sync_project_refs")
	require.NoError(t, err, "get effective sync: %s", syncOut)
	assert.Equal(t, "true", strings.TrimSpace(syncOut), "effective sync should reflect global")

	tagsOut, err := tc.RunCampInDir(dir, "settings", "get", "effective.commit.disable_commit_tags")
	require.NoError(t, err, "get effective disable-tags: %s", tagsOut)
	assert.Equal(t, "true", strings.TrimSpace(tagsOut), "effective disable-tags should reflect global")
}

// TestIntegration_CommitPrefs_LocalOverridesGlobal is the container twin of the
// former host TestEffectiveCommitPrefs_LocalOverridesGlobal: a campaign-local
// commit block fully replaces the global block for that campaign.
func TestIntegration_CommitPrefs_LocalOverridesGlobal(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commit-prefs-local"
	initCommitPrefsCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "settings", "set", "global.commit.sync_project_refs", "true")
	require.NoError(t, err, "set global sync")
	_, err = tc.RunCampInDir(dir, "settings", "set", "local.commit.sync_project_refs", "false")
	require.NoError(t, err, "set local sync")
	_, err = tc.RunCampInDir(dir, "settings", "set", "local.commit.disable_commit_tags", "true")
	require.NoError(t, err, "set local disable-tags")

	syncOut, err := tc.RunCampInDir(dir, "settings", "get", "effective.commit.sync_project_refs")
	require.NoError(t, err, "get effective sync: %s", syncOut)
	assert.Equal(t, "false", strings.TrimSpace(syncOut), "local should override sync to false")

	tagsOut, err := tc.RunCampInDir(dir, "settings", "get", "effective.commit.disable_commit_tags")
	require.NoError(t, err, "get effective disable-tags: %s", tagsOut)
	assert.Equal(t, "true", strings.TrimSpace(tagsOut), "local should override disable-tags to true")

	// Global stays intact; only the effective view is overridden per campaign.
	globalOut, err := tc.RunCampInDir(dir, "settings", "get", "global.commit.sync_project_refs")
	require.NoError(t, err, "get global sync: %s", globalOut)
	assert.Equal(t, "true", strings.TrimSpace(globalOut), "global sync should be unchanged")
}

// TestIntegration_CommitPrefs_LocalRoundTrip is the container twin of the former
// host TestLocalSettings_CommitRoundTrip: setting a local commit pref persists a
// commit block in .campaign/settings/local.json that reads back unchanged.
func TestIntegration_CommitPrefs_LocalRoundTrip(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commit-prefs-roundtrip"
	initCommitPrefsCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "settings", "set", "local.commit.sync_project_refs", "true")
	require.NoError(t, err, "set local sync")

	raw, err := tc.ReadFile(dir + "/.campaign/settings/local.json")
	require.NoError(t, err, "local.json should be readable")
	assert.Contains(t, raw, "commit", "local.json should carry a commit block")
	assert.Contains(t, raw, "sync_project_refs", "local.json should carry the sync pref")

	got, err := tc.RunCampInDir(dir, "settings", "get", "local.commit.sync_project_refs")
	require.NoError(t, err, "get local sync: %s", got)
	assert.Equal(t, "true", strings.TrimSpace(got), "local sync should round-trip")
}

// TestIntegration_CommitPrefs_LocalInheritClear covers unset vs explicit false
// vs whole-block clear: get reports "inherit" when local.Commit is absent,
// "false" when explicitly overridden, and "inherit" again after
// `settings set local.commit.* inherit` clears the full-replace block so
// effective falls back to global.
func TestIntegration_CommitPrefs_LocalInheritClear(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commit-prefs-inherit"
	initCommitPrefsCampaign(t, tc, dir)

	unsetOut, err := tc.RunCampInDir(dir, "settings", "get", "local.commit.sync_project_refs")
	require.NoError(t, err, "get unset local sync: %s", unsetOut)
	assert.Equal(t, "inherit", strings.TrimSpace(unsetOut),
		"unset local commit block must report inherit, not false")

	_, err = tc.RunCampInDir(dir, "settings", "set", "global.commit.sync_project_refs", "true")
	require.NoError(t, err, "set global sync")
	_, err = tc.RunCampInDir(dir, "settings", "set", "local.commit.sync_project_refs", "false")
	require.NoError(t, err, "set local sync false")

	localOut, err := tc.RunCampInDir(dir, "settings", "get", "local.commit.sync_project_refs")
	require.NoError(t, err, "get local sync: %s", localOut)
	assert.Equal(t, "false", strings.TrimSpace(localOut), "explicit local override should be false")

	clearOut, err := tc.RunCampInDir(dir, "settings", "set", "local.commit.sync_project_refs", "inherit")
	require.NoError(t, err, "set local sync inherit: %s", clearOut)

	afterClear, err := tc.RunCampInDir(dir, "settings", "get", "local.commit.sync_project_refs")
	require.NoError(t, err, "get after inherit: %s", afterClear)
	assert.Equal(t, "inherit", strings.TrimSpace(afterClear),
		"after inherit, local commit block must be cleared")

	effOut, err := tc.RunCampInDir(dir, "settings", "get", "effective.commit.sync_project_refs")
	require.NoError(t, err, "get effective after inherit: %s", effOut)
	assert.Equal(t, "true", strings.TrimSpace(effOut),
		"effective should fall back to global true after local clear")
}

// TestIntegration_CommitPrefs_MalformedLocalFailsClosed covers the safety fix:
// when the global default enables project-ref sync but the campaign-local
// override is unreadable, `camp p commit` must fail loudly rather than silently
// inherit the global policy and create an unexpected campaign-root pointer
// commit.
func TestIntegration_CommitPrefs_MalformedLocalFailsClosed(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commit-prefs-malformed"
	initCommitTagsCampaign(t, tc, dir) // init + git repo

	_, err := tc.RunCampInDir(dir, "settings", "set", "global.commit.sync_project_refs", "true")
	require.NoError(t, err, "enable global ref sync")

	_, err = tc.RunCampInDir(dir, "project", "new", "demo-app")
	require.NoError(t, err, "camp project new")
	require.NoError(t, tc.WriteFile(dir+"/projects/demo-app/foo.go", "package x\n"))

	// Corrupt the campaign-local commit override.
	require.NoError(t, tc.WriteFile(dir+"/.campaign/settings/local.json", "{ this is not valid json"))

	rootHeadBefore := tc.GitOutput(t, dir, "rev-parse", "HEAD")

	out, err := tc.RunCampInDir(dir+"/projects/demo-app", "p", "commit", "-m", "feat: stub")
	require.Error(t, err, "malformed local.json should fail the project commit; output:\n%s", out)

	rootHeadAfter := tc.GitOutput(t, dir, "rev-parse", "HEAD")
	assert.Equal(t, rootHeadBefore, rootHeadAfter,
		"no campaign-root pointer commit should be created when commit-pref resolution fails")
}
