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
