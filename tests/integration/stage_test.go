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

func TestStage_Root_BasicFile(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/stage-basic"

	_, err := tc.InitCampaign(campaignPath, "stage-basic", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(campaignPath+"/notes.md", "hello"))

	output, err := tc.RunCampInDir(campaignPath, "stage")
	require.NoError(t, err, "camp stage should succeed")
	assert.Contains(t, output, "Staging changes")
	assert.Contains(t, output, "Changes staged")

	staged := tc.GitOutput(t, campaignPath, "diff", "--cached", "--name-only")
	assert.Contains(t, staged, "notes.md", "notes.md should be staged")
}

func TestStage_Root_NothingToStage(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/stage-empty"

	_, err := tc.InitCampaign(campaignPath, "stage-empty", "product")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "stage")
	require.NoError(t, err)
	assert.Contains(t, output, "Nothing to stage")
}

func TestStage_Root_ExcludesSubmoduleRefs(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/stage-ref-excl"
	subPath := "/test/stage-sub-repo"

	_, err := tc.InitCampaign(campaignPath, "stage-ref-excl", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(subPath))

	tc.Shell(t, fmt.Sprintf(
		"cd %s && git -c protocol.file.allow=always submodule add %s projects/sub && "+
			"git commit -m 'add submodule'",
		campaignPath, subPath,
	))

	tc.Shell(t, fmt.Sprintf(
		"cd %s && echo bump > bump.txt && git add bump.txt && "+
			"git commit -m 'sub bump' && "+
			"cd %s/projects/sub && git fetch origin && git reset --hard origin/HEAD || true",
		subPath, campaignPath,
	))
	tc.Shell(t, fmt.Sprintf(
		"cd %s/projects/sub && echo local > local.txt && git add local.txt && "+
			"git commit -m 'sub local change'",
		campaignPath,
	))

	require.NoError(t, tc.WriteFile(campaignPath+"/root-note.md", "root content"))

	output, err := tc.RunCampInDir(campaignPath, "stage")
	require.NoError(t, err)
	assert.Contains(t, output, "Changes staged")

	staged := tc.GitOutput(t, campaignPath, "diff", "--cached", "--name-only")
	assert.Contains(t, staged, "root-note.md", "root file should be staged")
	assert.NotContains(t, staged, "projects/sub",
		"submodule ref should NOT be staged at campaign root by default")
}

func TestStage_Root_OnlyExcludedRefs_ReportsNothingStaged(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/stage-only-refs"
	subPath := "/test/stage-only-refs-sub"

	_, err := tc.InitCampaign(campaignPath, "stage-only-refs", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(subPath))

	tc.Shell(t, fmt.Sprintf(
		"cd %s && git -c protocol.file.allow=always submodule add %s projects/sub && "+
			"git commit -m 'add submodule'",
		campaignPath, subPath,
	))

	tc.Shell(t, fmt.Sprintf(
		"cd %s/projects/sub && echo local > local.txt && git add local.txt && "+
			"git commit -m 'sub local change'",
		campaignPath,
	))

	output, err := tc.RunCampInDir(campaignPath, "stage")
	require.NoError(t, err)
	assert.NotContains(t, output, "Changes staged",
		"with only excluded submodule refs dirty, output must NOT claim changes were staged")
	assert.Contains(t, output, "--include-refs",
		"output should point users at --include-refs when refs are the only pending change")

	staged := tc.GitOutput(t, campaignPath, "diff", "--cached", "--name-only")
	assert.Empty(t, strings.TrimSpace(staged),
		"index should remain empty when only excluded submodule refs are dirty")
}

func TestStage_Root_IncludeRefs(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/stage-include-refs"
	subPath := "/test/stage-include-sub"

	_, err := tc.InitCampaign(campaignPath, "stage-include-refs", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(subPath))

	tc.Shell(t, fmt.Sprintf(
		"cd %s && git -c protocol.file.allow=always submodule add %s projects/sub && "+
			"git commit -m 'add submodule'",
		campaignPath, subPath,
	))

	tc.Shell(t, fmt.Sprintf(
		"cd %s/projects/sub && echo local > local.txt && git add local.txt && "+
			"git commit -m 'sub local change'",
		campaignPath,
	))

	output, err := tc.RunCampInDir(campaignPath, "stage", "--include-refs")
	require.NoError(t, err)
	assert.Contains(t, output, "Changes staged")

	staged := tc.GitOutput(t, campaignPath, "diff", "--cached", "--name-only")
	assert.Contains(t, staged, "projects/sub",
		"submodule ref should be staged with --include-refs")
}

func TestStage_Project_FromInsideProject(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/stage-proj"
	subPath := "/test/stage-proj-sub"

	_, err := tc.InitCampaign(campaignPath, "stage-proj", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(subPath))

	tc.Shell(t, fmt.Sprintf(
		"cd %s && git -c protocol.file.allow=always submodule add %s projects/sub && "+
			"git commit -m 'add submodule'",
		campaignPath, subPath,
	))

	require.NoError(t, tc.WriteFile(campaignPath+"/projects/sub/feature.md", "feature"))

	output, err := tc.RunCampInDir(campaignPath+"/projects/sub", "project", "stage")
	require.NoError(t, err, "camp project stage from inside project should succeed")
	assert.Contains(t, output, "Project changes staged")

	staged := tc.GitOutput(t, campaignPath+"/projects/sub", "diff", "--cached", "--name-only")
	assert.Contains(t, staged, "feature.md")

	rootStaged := tc.GitOutput(t, campaignPath, "diff", "--cached", "--name-only")
	assert.NotContains(t, rootStaged, "projects/sub",
		"campaign root should not have submodule ref staged by project stage")
}

func TestStage_Project_WithProjectFlag(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/stage-proj-flag"
	subPath := "/test/stage-proj-flag-sub"

	_, err := tc.InitCampaign(campaignPath, "stage-proj-flag", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(subPath))

	tc.Shell(t, fmt.Sprintf(
		"cd %s && git -c protocol.file.allow=always submodule add %s projects/sub && "+
			"git commit -m 'add submodule'",
		campaignPath, subPath,
	))

	require.NoError(t, tc.WriteFile(campaignPath+"/projects/sub/feature.md", "feature"))

	output, err := tc.RunCampInDir(campaignPath, "project", "stage", "--project", "sub")
	require.NoError(t, err)
	assert.Contains(t, output, "Project changes staged")

	staged := tc.GitOutput(t, campaignPath+"/projects/sub", "diff", "--cached", "--name-only")
	assert.Contains(t, staged, "feature.md")
}

func TestStage_Root_StaleLockRecovery(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/stage-stale-lock"

	_, err := tc.InitCampaign(campaignPath, "stage-stale-lock", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(campaignPath+"/note.md", "stale-lock content"))

	tc.Shell(t, fmt.Sprintf(
		"touch -d '5 minutes ago' %s/.git/index.lock 2>/dev/null || "+
			"touch -t 197001010001 %s/.git/index.lock",
		campaignPath, campaignPath,
	))

	output, err := tc.RunCampInDir(campaignPath, "stage")
	require.NoError(t, err, "camp stage should recover from a stale lock")
	assert.Contains(t, output, "Changes staged")

	exists, err := tc.CheckFileExists(campaignPath + "/.git/index.lock")
	require.NoError(t, err)
	assert.False(t, exists, "stale index.lock should be removed by retry logic")

	staged := tc.GitOutput(t, campaignPath, "diff", "--cached", "--name-only")
	assert.Contains(t, strings.TrimSpace(staged), "note.md")
}
