//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_WorkflowCreateCustomWorkflow(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/test/workflow-create"
	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Workflow Create Test",
		"--type", "product",
		"-d", "Workflow create integration",
		"-m", "Verify custom workflow creation",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	out, err := tc.RunCampInDir(campaignDir,
		"workflow", "create", "research",
		"--shortcut", "re",
		"--title", "Research",
	)
	require.NoError(t, err, "camp workflow create: %s", out)
	assert.Contains(t, out, "created workflow/research")
	assert.Contains(t, out, "shortcut: re -> workflow/research/")
	assert.Contains(t, out, "workitem type: research")

	jumps, err := tc.ReadFile(campaignDir + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	assert.Contains(t, jumps, "re:")
	assert.Contains(t, jumps, "path: workflow/research/")

	campaignYAML, err := tc.ReadFile(campaignDir + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.Contains(t, campaignYAML, "name: research")
	assert.Contains(t, campaignYAML, "path: workflow/research/")

	out, err = tc.RunCampInDir(campaignDir,
		"workitem", "create", "compare-llms",
		"--type", "research",
		"--title", "Compare LLMs",
	)
	require.NoError(t, err, "camp workitem create custom type: %s", out)
	assert.Contains(t, out, "created workflow/research/compare-llms")

	out, err = tc.RunCampInDir(campaignDir, "complete", "re")
	require.NoError(t, err, "camp complete re: %s", out)
	assert.Contains(t, out, "compare-llms")

	out, err = tc.RunCampInDir(campaignDir, "complete", "research")
	require.NoError(t, err, "camp complete research: %s", out)
	assert.Contains(t, out, "compare-llms")

	out, err = tc.RunCampInDir(campaignDir, "go", "re", "compare-llms", "--print")
	require.NoError(t, err, "camp go re compare-llms --print: %s", out)
	assert.Contains(t, out, "workflow/research/compare-llms")
}
