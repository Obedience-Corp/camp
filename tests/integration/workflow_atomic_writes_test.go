//go:build integration

package integration

import (
	"strings"
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

// TestIntegration_WorkflowConfigSaveAtomic_ENOSPC proves SaveJumpsConfig
// honors the atomic-write contract: when the rename step fails, the original
// file is preserved and no tmp residue is left behind.
//
// The CW0003 spec called for an ENOSPC trigger via tmpfs bind-mount, which
// requires container privileges we do not grant. We get the same atomicity
// signal by replacing the destination file with a non-empty directory, which
// makes os.Rename fail with EISDIR/ENOTEMPTY on every Linux kernel.
func TestIntegration_WorkflowConfigSaveAtomic_ENOSPC(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workflow-atomic-enospc"
	initWorkflowCampaign(t, tc, dir)

	jumpsPath := dir + "/.campaign/settings/jumps.yaml"
	_, err := tc.ReadFile(jumpsPath)
	if err != nil {
		_, err = tc.RunCampInDir(dir, "workflow", "create", "research",
			"--shortcut", "re", "--title", "Research")
		require.NoError(t, err)
	}
	baseline, err := tc.ReadFile(jumpsPath)
	require.NoError(t, err)
	require.NotEmpty(t, baseline)

	_, _, err = tc.ExecCommand("sh", "-c",
		"rm -f "+jumpsPath+" && mkdir -p "+jumpsPath+"/blocker && touch "+jumpsPath+"/blocker/marker")
	require.NoError(t, err)

	out, runErr := tc.RunCampInDir(dir, "workflow", "create", "design",
		"--shortcut", "de", "--title", "Design")
	require.Error(t, runErr, "expected atomic-save failure when destination is a non-empty directory; got: %s", out)
	assert.True(t,
		strings.Contains(out, "failed to write") ||
			strings.Contains(out, "directory not empty") ||
			strings.Contains(out, "is a directory") ||
			strings.Contains(out, "file exists"),
		"expected atomic-write error surface, got: %s", out)

	_, exit, err := tc.ExecCommand("test", "-d", jumpsPath)
	require.NoError(t, err)
	assert.Equal(t, 0, exit, "destination must remain a directory (failed rename did not tunnel through)")

	_, exit, err = tc.ExecCommand("test", "-f", jumpsPath+"/blocker/marker")
	require.NoError(t, err)
	assert.Equal(t, 0, exit, "marker inside destination dir must survive failed save (no clobber)")

	leftovers, _, err := tc.ExecCommand("sh", "-c",
		"ls "+dir+"/.campaign/settings/ 2>/dev/null | (grep 'jumps.yaml.tmp-' || true) | wc -l | tr -d ' '")
	require.NoError(t, err)
	assert.Equal(t, "0", strings.TrimSpace(leftovers),
		"atomic write must clean up tmp file on rename failure; found leftover tmp files: %s", leftovers)
}

