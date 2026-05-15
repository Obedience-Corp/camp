//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_WorkitemCreateAndAdopt(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/test/workitem-create"
	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Workitem Create Test",
		"--type", "product",
		"-d", "Workitem create+adopt integration",
		"-m", "Verify create and adopt subcommands",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	t.Run("CreateBuildsDirectoryAndWorkitem", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "demo-feature", "--type", "feature", "--title", "Demo")
		require.NoError(t, err, "camp workitem create: %s", out)
		assert.Contains(t, out, "created workflow/feature/demo-feature")
		assert.Contains(t, out, "id: feature-demo-feature-")
		assert.Contains(t, out, "type: feature")

		manifest, err := tc.ReadFile(campaignDir + "/workflow/feature/demo-feature/.workitem")
		require.NoError(t, err)
		assert.Contains(t, manifest, "version: v1alpha5")
		assert.Contains(t, manifest, "kind: workitem")
		assert.Contains(t, manifest, "type: feature")
		assert.Contains(t, manifest, "title: Demo")
	})

	t.Run("CreateRefusesExistingDirectory", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "demo-feature", "--type", "feature")
		require.Error(t, err, "expected error for existing dir")
		assert.True(t,
			strings.Contains(out, "target directory already exists") || strings.Contains(out, "already exists"),
			"error should mention existing dir, got: %s", out)
	})

	t.Run("CreateRejectsInvalidSlug", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "create", "Bad Slug!")
		require.Error(t, err, "expected error for invalid slug")
		assert.Contains(t, out, "invalid slug")
	})

	t.Run("AdoptAddsMarkerToExistingDir", func(t *testing.T) {
		_, _, err := tc.ExecCommand("mkdir", "-p", campaignDir+"/workflow/incident/p99-spike")
		require.NoError(t, err)
		out, err := tc.RunCampInDir(campaignDir, "workitem", "adopt", "workflow/incident/p99-spike", "--type", "incident", "--title", "P99 spike")
		require.NoError(t, err, "camp workitem adopt: %s", out)
		assert.Contains(t, out, "adopted workflow/incident/p99-spike")

		manifest, err := tc.ReadFile(campaignDir + "/workflow/incident/p99-spike/.workitem")
		require.NoError(t, err)
		assert.Contains(t, manifest, "type: incident")
		assert.Contains(t, manifest, "title: P99 spike")
	})

	t.Run("AdoptRefusesAlreadyAdopted", func(t *testing.T) {
		out, err := tc.RunCampInDir(campaignDir, "workitem", "adopt", "workflow/incident/p99-spike", "--type", "incident")
		require.Error(t, err, "expected error for already-adopted dir")
		assert.Contains(t, out, "already")
	})
}
