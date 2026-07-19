//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_LinkConcurrentNoLoss(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-link-concurrent"

	_, err := tc.RunCamp(
		"init", dir,
		"--name", "Link Concurrent",
		"--type", "product",
		"-d", "Concurrent link race",
		"-m", "Verify WithLock holds RMW",
		"--force", "--no-register", "--no-git",
	)
	require.NoError(t, err)

	out, err := tc.RunCampInDir(dir, "workitem", "create", "shared",
		"--type", "design", "--title", "shared")
	require.NoError(t, err, "create shared: %s", out)

	const n = 20
	for i := 0; i < n; i++ {
		_, _, err = tc.ExecCommand("mkdir", "-p", fmt.Sprintf("%s/projects/proj-%02d", dir, i))
		require.NoError(t, err)
	}

	var script strings.Builder
	script.WriteString("set -e; cd " + dir + "; pids=\"\"; ")
	for i := 0; i < n; i++ {
		// blocked_by (not the deprecated related+project) exercises the same
		// concurrent many-row append path this test guards.
		fmt.Fprintf(&script,
			"/camp workitem link shared --project proj-%02d --role blocked_by >/tmp/link-%02d.out 2>&1 & pids=\"$pids $!\"; ",
			i, i)
	}
	script.WriteString("for p in $pids; do wait $p || exit $?; done")

	out, _, err = tc.ExecCommand("sh", "-c", script.String())
	logsOut, _, _ := tc.ExecCommand("sh", "-c",
		"for f in /tmp/link-*.out; do echo \"=== $f ===\"; cat \"$f\"; done")
	if err != nil {
		t.Fatalf("concurrent link script failed: %s\n%s", out, logsOut)
	}
	t.Logf("per-invocation logs:\n%s", logsOut)

	listOut, err := tc.RunCampInDir(dir, "workitem", "links")
	require.NoError(t, err, "links list: %s", listOut)
	t.Logf("links list:\n%s", listOut)

	for i := 0; i < n; i++ {
		project := fmt.Sprintf("proj-%02d", i)
		assert.Contains(t, listOut, project,
			"link to %s missing from registry after concurrent race", project)
	}

	linksYAML, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	count := strings.Count(linksYAML, "id: lnk_")
	assert.Equal(t, n, count, "expected %d links in registry, found %d:\n%s", n, count, linksYAML)
}
