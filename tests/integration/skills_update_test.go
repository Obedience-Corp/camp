//go:build integration
// +build integration

package integration

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	staleCommitSkill = "---\nname: campaign-commit\n---\nAt the campaign root use standard git commit.\n"
	localSkillRule   = "My local rule: always run the project gate first."
	skillsManifest   = "/.campaign/skills/.shipped.yaml"
)

func initSkillsUpdateCamp(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()
	path := "/campaigns/" + name
	_, err := tc.InitCampaign(path, name, "product")
	require.NoError(t, err)
	return path
}

func skillFile(path, slug string) string {
	return path + "/.campaign/skills/" + slug + "/SKILL.md"
}

func sha256In(t *testing.T, tc *TestContainer, file string) string {
	t.Helper()
	return strings.TrimSpace(tc.Shell(t, "sha256sum "+file+" | cut -d' ' -f1"))
}

func commitSkillsFixture(t *testing.T, tc *TestContainer, path string) {
	t.Helper()
	tc.Shell(t, "cd "+path+" && git add -A && git commit -qm 'skills fixture' && sync")
}

func appendLocalRule(t *testing.T, tc *TestContainer, path, slug string) {
	t.Helper()
	tc.Shell(t, fmt.Sprintf("printf '\\n%s\\n' >> %s && sync", localSkillRule, skillFile(path, slug)))
}

// makeOutdated rewrites a bundle the way an older camp shipped it: stale
// content on disk, with that content's hash recorded in the manifest.
func makeOutdated(t *testing.T, tc *TestContainer, path, slug string) (shippedHash string) {
	t.Helper()
	file := skillFile(path, slug)
	shippedHash = sha256In(t, tc, file)
	require.NoError(t, tc.WriteFile(file, staleCommitSkill))
	staleHash := sha256In(t, tc, file)
	tc.Shell(t, fmt.Sprintf("sed -i 's/%s/%s/' %s%s && sync", shippedHash, staleHash, path, skillsManifest))
	return shippedHash
}

func bundleStateLine(t *testing.T, statusOut, slug string) string {
	t.Helper()
	for line := range strings.SplitSeq(statusOut, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == ".campaign/skills/"+slug {
			return fields[1]
		}
	}
	t.Fatalf("no shipped bundle row for %s in status:\n%s", slug, statusOut)
	return ""
}

func TestSkillsUpdate_InitRecordsShippedManifest(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/skills-update-manifest"
	_, err := tc.RunCamp("init", path, "--name", "skills-update-manifest", "-d", "d", "-m", "m", "--type", "product")
	require.NoError(t, err)

	manifest, err := tc.ReadFile(path + skillsManifest)
	require.NoError(t, err)
	assert.Contains(t, manifest, "schema_version: skills-shipped/v1alpha1")
	assert.Contains(t, manifest, "camp_version: ")
	assert.Contains(t, manifest, "SKILL.md: "+sha256In(t, tc, skillFile(path, "campaign-commit")))

	assert.NotEmpty(t, tc.GitOutput(t, path, "log", "--format=%s", "--", ".campaign/skills/.shipped.yaml"),
		"camp init's own commit must include the manifest")
	assert.Empty(t, tc.GitOutput(t, path, "status", "--porcelain"))

	status, err := tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err, status)
	for _, slug := range []string{"campaign-commit", "campaign-workflows", "fest-execution"} {
		assert.Equal(t, "current", bundleStateLine(t, status, slug))
	}

	statusJSON, err := tc.RunCampInDir(path, "skills", "status", "--json")
	require.NoError(t, err, statusJSON)
	assert.Contains(t, statusJSON, `"tool": "bundle"`)
	assert.Contains(t, statusJSON, `"path": ".campaign/skills/campaign-commit"`)
	assert.Contains(t, statusJSON, `"tool": "claude"`, "bundle rows are added beside the projection rows")
}

func TestSkillsUpdate_RefreshesOutdatedFile(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "skills-update-outdated")
	shippedHash := makeOutdated(t, tc, path, "campaign-commit")
	commitSkillsFixture(t, tc, path)
	manifestBefore := sha256In(t, tc, path+skillsManifest)

	status, err := tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err, status)
	assert.Equal(t, "outdated", bundleStateLine(t, status, "campaign-commit"))

	dry, err := tc.RunCampInDir(path, "skills", "update", "--dry-run")
	require.NoError(t, err, dry)
	t.Logf("camp skills update --dry-run:\n%s", dry)
	assert.Contains(t, dry, "would update .campaign/skills/campaign-commit/SKILL.md (outdated)")
	assert.Contains(t, dry, "dry run: nothing written")
	assert.NotEqual(t, shippedHash, sha256In(t, tc, skillFile(path, "campaign-commit")))
	assert.Equal(t, manifestBefore, sha256In(t, tc, path+skillsManifest))

	out, err := tc.RunCampInDir(path, "skills", "update")
	require.NoError(t, err, out)
	assert.Contains(t, out, "updated .campaign/skills/campaign-commit/SKILL.md (outdated)")
	assert.Contains(t, out, "recorded .campaign/skills/.shipped.yaml")
	assert.Equal(t, shippedHash, sha256In(t, tc, skillFile(path, "campaign-commit")))
	manifest, err := tc.ReadFile(path + skillsManifest)
	require.NoError(t, err)
	assert.Contains(t, manifest, "SKILL.md: "+shippedHash)

	status, err = tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err, status)
	assert.Equal(t, "current", bundleStateLine(t, status, "campaign-commit"))
}

func TestSkillsUpdate_PreservesModifiedFile(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "skills-update-modified")
	appendLocalRule(t, tc, path, "fest-execution")
	commitSkillsFixture(t, tc, path)

	status, err := tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err, status)
	assert.Equal(t, "modified", bundleStateLine(t, status, "fest-execution"))

	out, err := tc.RunCampInDir(path, "skills", "update")
	require.NoError(t, err, "preserved files are reported, not a failure: %s", out)
	assert.Contains(t, out, "preserved .campaign/skills/fest-execution/SKILL.md (changed locally); run 'camp skills update --force fest-execution' to replace it")
	content, err := tc.ReadFile(skillFile(path, "fest-execution"))
	require.NoError(t, err)
	assert.Contains(t, content, localSkillRule)

	repair, err := tc.RunCampInDir(path, "init", "--repair", "--dry-run")
	require.NoError(t, err, repair)
	assert.Contains(t, repair, "modified locally, preserved; run camp skills update --force fest-execution to replace")
	assert.NotContains(t, repair, "nothing to repair")
}

func TestSkillsUpdate_ForceCommittedBundlePrintsGitUndo(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "skills-update-force-git")
	appendLocalRule(t, tc, path, "fest-execution")
	commitSkillsFixture(t, tc, path)

	out, err := tc.RunCampInDir(path, "skills", "update", "--force", "fest-execution")
	require.NoError(t, err, out)
	assert.Contains(t, out, "replaced .campaign/skills/fest-execution/SKILL.md (changed locally)")
	assert.Contains(t, out, "undo fest-execution: git checkout HEAD -- .campaign/skills/fest-execution")
	assert.NotContains(t, out, ".backup")
	content, err := tc.ReadFile(skillFile(path, "fest-execution"))
	require.NoError(t, err)
	assert.NotContains(t, content, localSkillRule)

	tc.Shell(t, "cd "+path+" && git checkout HEAD -- .campaign/skills/fest-execution")
	content, err = tc.ReadFile(skillFile(path, "fest-execution"))
	require.NoError(t, err)
	assert.Contains(t, content, localSkillRule, "the printed undo must restore the local edit")
}

func TestSkillsUpdate_ForceUncommittedEditBacksUp(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "skills-update-force-backup")
	appendLocalRule(t, tc, path, "fest-execution")

	out, err := tc.RunCampInDir(path, "skills", "update", "--force", "fest-execution")
	require.NoError(t, err, out)
	match := regexp.MustCompile(`backed up replaced fest-execution files to (\.campaign/skills/\.backup/fest-execution-\d{8}T\d{6}Z)`).FindStringSubmatch(out)
	require.Len(t, match, 2, "force outside git must print the backup path:\n%s", out)
	undo := regexp.MustCompile(`undo fest-execution: (cp -R \S+ \S+)`).FindStringSubmatch(out)
	require.Len(t, undo, 2, "force must print a one-command undo:\n%s", out)

	backup, err := tc.ReadFile(path + "/" + match[1] + "/SKILL.md")
	require.NoError(t, err)
	assert.Contains(t, backup, localSkillRule)
	live, err := tc.ReadFile(skillFile(path, "fest-execution"))
	require.NoError(t, err)
	assert.NotContains(t, live, localSkillRule)

	tc.Shell(t, "cd "+path+" && "+undo[1])
	live, err = tc.ReadFile(skillFile(path, "fest-execution"))
	require.NoError(t, err)
	assert.Contains(t, live, localSkillRule, "the printed undo must restore the local edit")
}

func TestSkillsUpdate_DryRunWritesNothing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "skills-update-dry-run")
	appendLocalRule(t, tc, path, "fest-execution")
	tc.Shell(t, "rm -rf "+path+"/.campaign/skills/camp-navigation "+path+skillsManifest+" && sync")

	snapshot := "cd " + path + " && find .campaign/skills | sort && find .campaign/skills -type f | sort | xargs sha256sum"
	before := tc.Shell(t, snapshot)

	out, err := tc.RunCampInDir(path, "skills", "update", "--dry-run", "--force")
	require.NoError(t, err, out)
	assert.Contains(t, out, "would add .campaign/skills/camp-navigation/SKILL.md (missing)")
	assert.Contains(t, out, "would replace .campaign/skills/fest-execution/SKILL.md")
	assert.Equal(t, before, tc.Shell(t, snapshot), "--dry-run must not write files, backups, or the manifest")
	exists, err := tc.CheckFileExists(path + skillsManifest)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSkillsUpdate_LegacyCampWithoutManifest(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "skills-update-legacy")
	require.NoError(t, tc.WriteFile(skillFile(path, "campaign-commit"), staleCommitSkill))
	tc.Shell(t, "rm -f "+path+skillsManifest+" && sync")
	commitSkillsFixture(t, tc, path)

	status, err := tc.RunCampInDir(path, "skills", "status")
	require.NoError(t, err, status)
	assert.Equal(t, "unknown", bundleStateLine(t, status, "campaign-commit"))
	assert.Equal(t, "current", bundleStateLine(t, status, "fest-execution"))

	dry, err := tc.RunCampInDir(path, "skills", "update", "--dry-run")
	require.NoError(t, err, dry)
	t.Logf("legacy camp skills update --dry-run:\n%s", dry)
	assert.Contains(t, dry, "preserved .campaign/skills/campaign-commit/SKILL.md")

	repairDry, err := tc.RunCampInDir(path, "init", "--repair", "--dry-run")
	require.NoError(t, err, repairDry)
	t.Logf("legacy camp init --repair --dry-run:\n%s", repairDry)
	assert.Contains(t, repairDry, ".campaign/skills/campaign-commit/SKILL.md")
	assert.Contains(t, repairDry, "modified or outdated, run camp skills update --force campaign-commit")
	assert.NotContains(t, repairDry, "nothing to repair")

	_, err = tc.RunCampInDir(path, "init", "--repair", "--yes")
	require.NoError(t, err)
	content, err := tc.ReadFile(skillFile(path, "campaign-commit"))
	require.NoError(t, err)
	assert.Equal(t, staleCommitSkill, content, "repair must preserve a file it cannot prove is unmodified")
	assert.NotEmpty(t, tc.GitOutput(t, path, "ls-files", ".campaign/skills/.shipped.yaml"),
		"repair must record and commit the manifest")
}

func TestSkillsUpdate_UnknownSlugListsShippedBundles(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "skills-update-unknown-slug")

	out, err := tc.RunCampInDir(path, "skills", "update", "no-such-bundle")
	require.Error(t, err)
	assert.Contains(t, out, "unknown shipped skill bundle no-such-bundle")
	assert.Regexp(t, `shipped bundles: .*\bcampaign-commit\b`, out)
}

func TestInitRepair_UpdatesOutdatedSkillAndCommits(t *testing.T) {
	tc := GetSharedContainer(t)
	path := initSkillsUpdateCamp(t, tc, "skills-update-repair")
	shippedHash := makeOutdated(t, tc, path, "campaign-commit")
	commitSkillsFixture(t, tc, path)

	dry, err := tc.RunCampInDir(path, "init", "--repair", "--dry-run")
	require.NoError(t, err, dry)
	t.Logf("outdated camp init --repair --dry-run:\n%s", dry)
	assert.Contains(t, dry, ".campaign/skills/campaign-commit/SKILL.md")
	assert.Contains(t, dry, "outdated shipped skill, update to this camp version")

	out, err := tc.RunCampInDir(path, "init", "--repair", "--yes")
	require.NoError(t, err, out)
	assert.Equal(t, shippedHash, sha256In(t, tc, skillFile(path, "campaign-commit")))
	assert.Empty(t, tc.GitOutput(t, path, "status", "--porcelain", "--", ".campaign/skills"),
		"repair must commit the refreshed skill and manifest")
}
