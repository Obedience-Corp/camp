//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	legacyCommitPath = ".campaign/skills/campaign-commit/SKILL.md"
	legacyGitNote    = "(unchanged since camp wrote it, per git history)"
)

// makeLegacyCamp rebuilds a fresh camp the way a camp older than the manifest
// looks: no .shipped.yaml, and campaign-commit holding stale content that the
// camp's only commit wrote. It returns the hash of the shipped content.
func makeLegacyCamp(t *testing.T, tc *TestContainer, path string) (shippedHash string) {
	t.Helper()
	shippedHash = sha256In(t, tc, skillFile(path, "campaign-commit"))
	require.NoError(t, tc.WriteFile(skillFile(path, "campaign-commit"), staleCommitSkill))
	tc.Shell(t, "cd "+path+" && rm -rf .git "+path+skillsManifest+
		" && git init -q && git add -A && git commit -qm 'camp init' && sync")
	return shippedHash
}

func TestSkillsUpdate_LegacyCampUpdatesOnGitEvidence(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "skills-legacy-git-evidence")
	shippedHash := makeLegacyCamp(t, tc, path)

	status, err := tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err, status)
	assert.Equal(t, "outdated", bundleStateLine(t, status, "campaign-commit"))
	assert.Equal(t, "current", bundleStateLine(t, status, "fest-execution"))

	dry, err := tc.RunCampInDir(path, "skills", "update", "--dry-run")
	require.NoError(t, err, dry)
	t.Logf("legacy camp skills update --dry-run:\n%s", dry)
	assert.Contains(t, dry, "would update "+legacyCommitPath+" "+legacyGitNote)
	exists, err := tc.CheckFileExists(path + skillsManifest)
	require.NoError(t, err)
	assert.False(t, exists, "--dry-run must not record the manifest")

	out, err := tc.RunCampInDir(path, "skills", "update")
	require.NoError(t, err, out)
	t.Logf("legacy camp skills update:\n%s", out)
	assert.Contains(t, out, "updated "+legacyCommitPath+" "+legacyGitNote)
	assert.Contains(t, out, "undo campaign-commit: git checkout HEAD -- "+legacyCommitPath+
		" (camp skills status then shows campaign-commit as modified)")
	assert.Contains(t, out, "recorded .campaign/skills/.shipped.yaml")
	assert.NotContains(t, out, "preserved")
	assert.Equal(t, shippedHash, sha256In(t, tc, skillFile(path, "campaign-commit")))
	manifest, err := tc.ReadFile(path + skillsManifest)
	require.NoError(t, err)
	assert.Contains(t, manifest, "SKILL.md: "+shippedHash)

	status, err = tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err, status)
	assert.Equal(t, "current", bundleStateLine(t, status, "campaign-commit"))

	tc.Shell(t, "cd "+path+" && git checkout HEAD -- "+legacyCommitPath)
	content, err := tc.ReadFile(skillFile(path, "campaign-commit"))
	require.NoError(t, err)
	assert.Equal(t, staleCommitSkill, content, "the printed undo must restore the replaced file")
	status, err = tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err, status)
	assert.Equal(t, "modified", bundleStateLine(t, status, "campaign-commit"), "the undo note must describe what status shows")
}

func TestSkillsUpdate_LegacyCampWithoutGitEvidenceStaysUnknown(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "skills-legacy-no-evidence")
	makeLegacyCamp(t, tc, path)
	appendLocalRule(t, tc, path, "campaign-commit")

	status, err := tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err, status)
	assert.Equal(t, "unknown", bundleStateLine(t, status, "campaign-commit"), "an uncommitted edit is not camp's")

	out, err := tc.RunCampInDir(path, "skills", "update")
	require.NoError(t, err, out)
	assert.Contains(t, out, "preserved "+legacyCommitPath+
		" (differs, no record or git history of what camp shipped); run 'camp skills update --force campaign-commit' to replace it")
	content, err := tc.ReadFile(skillFile(path, "campaign-commit"))
	require.NoError(t, err)
	assert.Contains(t, content, localSkillRule)

	tc.Shell(t, "cd "+path+" && git checkout HEAD -- "+legacyCommitPath+" && rm -rf .git "+path+skillsManifest+" && sync")
	status, err = tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err, status)
	assert.Equal(t, "unknown", bundleStateLine(t, status, "campaign-commit"), "without git there is no evidence")
}

func TestInitRepair_LegacyCampUpdatesOnGitEvidenceAndCommits(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "skills-legacy-repair")
	shippedHash := makeLegacyCamp(t, tc, path)

	dry, err := tc.RunCampInDir(path, "init", "--repair", "--dry-run")
	require.NoError(t, err, dry)
	t.Logf("legacy camp init --repair --dry-run:\n%s", dry)
	assert.Contains(t, dry, legacyCommitPath)
	assert.Contains(t, dry, "unchanged since camp wrote it per git history, update to this camp version")
	assert.Contains(t, dry, "record shipped skill hashes")
	assert.NotContains(t, dry, "modified or outdated, run camp skills update --force campaign-commit")

	out, err := tc.RunCampInDir(path, "init", "--repair", "--yes")
	require.NoError(t, err, out)
	assert.Equal(t, shippedHash, sha256In(t, tc, skillFile(path, "campaign-commit")))
	assert.NotEmpty(t, tc.GitOutput(t, path, "ls-files", ".campaign/skills/.shipped.yaml"),
		"repair must record and commit the manifest")
	assert.Empty(t, tc.GitOutput(t, path, "status", "--porcelain", "--", ".campaign/skills"),
		"repair must commit the refreshed skill and manifest")

	status, err := tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err, status)
	assert.Equal(t, "current", bundleStateLine(t, status, "campaign-commit"))
}
