//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_WorkitemProjects(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/test/workitem-projects"
	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Workitem Projects Test",
		"--type", "product",
		"-d", "Projects integration",
		"-m", "Verify --project normalization, dedupe, and validation",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	t.Run("CreateDedupesProjects", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "dedup-item",
			"--type", "feature", "--title", "Dedup",
			"--project", "projects/camp", "--project", "projects/camp")
		require.NoError(t, err, "create --project: %s", out)

		marker, err := tc.ReadFile(campaignDir + "/workflow/feature/dedup-item/.workitem")
		require.NoError(t, err)
		assert.Contains(t, marker, "projects:")
		assert.Equal(t, 1, strings.Count(marker, "projects/camp"),
			"duplicate project paths must collapse to one entry")
	})

	t.Run("CreateNormalizesTrailingSlashAndDedupes", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "slash-item",
			"--type", "feature", "--title", "Slash",
			"--project", "projects/camp", "--project", "projects/camp/")
		require.NoError(t, err, "create --project: %s", out)

		marker, err := tc.ReadFile(campaignDir + "/workflow/feature/slash-item/.workitem")
		require.NoError(t, err)
		assert.NotContains(t, marker, "projects/camp/", "trailing slash must be normalized away")
		assert.Equal(t, 1, strings.Count(marker, "projects/camp"),
			"trailing-slash variant must normalize then dedupe to a single entry")
	})

	t.Run("CreateRejectsEscapingProjectAndCreatesNoDir", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "escaping-item",
			"--type", "feature", "--title", "Escaping", "--project", "../outside")
		require.Error(t, err, "create with an escaping --project must fail: %s", out)

		_, exitCode, execErr := tc.ExecCommand("test", "-d", campaignDir+"/workflow/feature/escaping-item")
		require.NoError(t, execErr, "directory-absence check should execute")
		require.NotEqual(t, 0, exitCode, "no directory should be created when validation fails")
	})

	t.Run("CreateRejectsBareNameWithoutProjectsPrefix", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "bare-proj",
			"--type", "feature", "--title", "Bare", "--project", "camp")
		require.Error(t, err, "create --project camp must fail: %s", out)
		assert.Contains(t, out, "projects/")
		_, exitCode, execErr := tc.ExecCommand("test", "-d", campaignDir+"/workflow/feature/bare-proj")
		require.NoError(t, execErr)
		require.NotEqual(t, 0, exitCode, "no directory should be created when prefix validation fails")
	})

	t.Run("CreateRejectsWorktreePath", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "wt-proj",
			"--type", "feature", "--title", "WT", "--project", "projects/worktrees/camp/feat")
		require.Error(t, err, "create --project worktree path must fail: %s", out)
		assert.Contains(t, out, "worktrees")
	})

	t.Run("AdoptRejectsBareNameWithoutProjectsPrefix", func(t *testing.T) {
		_, _, err := tc.ExecCommand("mkdir", "-p", campaignDir+"/scratch/adopt-me")
		require.NoError(t, err)
		out, err := tc.RunCampInDir(campaignDir, "workitem", "adopt", "scratch/adopt-me",
			"--type", "feature", "--title", "Adopt", "--project", "camp")
		require.Error(t, err, "adopt --project camp must fail: %s", out)
		assert.Contains(t, out, "projects/")
	})
}

func TestIntegration_WorkitemProjectsDoctorWarning(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/test/workitem-projects-doctor"
	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Workitem Projects Doctor Test",
		"--type", "product",
		"-d", "Projects doctor integration",
		"-m", "Verify doctor warns on a missing project path and still exits 0",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "doc-item",
		"--type", "feature", "--title", "DocItem", "--project", "projects/nonexistent")
	require.NoError(t, err, "create --project: %s", out)

	dout, derr := tc.RunCampInDir(campaignDir, "workitem", "doctor")
	require.NoError(t, derr, "doctor must exit 0 when the only issue is a missing project path:\n%s", dout)
	assert.Equal(t, 1, strings.Count(dout, "workitem.project.not-found"),
		"expected exactly one missing-project finding:\n%s", dout)
	assert.Contains(t, dout, "[warning] workitem.project.not-found",
		"a missing project path must be a warning, never an error")
}
