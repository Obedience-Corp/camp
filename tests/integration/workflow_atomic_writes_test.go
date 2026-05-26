//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_WorkflowConfigSave_NoTempLeftover(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workflow-atomic-leftover"
	initWorkflowCampaign(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workflow", "create", "research",
		"--shortcut", "re", "--title", "Research")
	require.NoError(t, err, "workflow create: %s", out)

	out, _, err = tc.ExecCommand("sh", "-c",
		"ls -A "+dir+"/.campaign/settings/ "+dir+"/.campaign/")
	require.NoError(t, err)
	assert.NotContains(t, out, ".tmp-",
		"WriteFileAtomically should not leave .tmp-* files after a successful save")
}

