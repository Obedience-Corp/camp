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

func TestSyncDetectsStaleLocalBranchDrift(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, barePath := setupSyncFallbackCampaign(t, tc, "sync-drift")

	var shas struct {
		recorded string
		stale    string
	}
	out := tc.Shell(t, fmt.Sprintf(`
set -e
seed=/test/sync-drift-seed
printf 'second\n' > "$seed/second.txt"
git -C "$seed" add second.txt
git -C "$seed" commit -m 'second'
git -C "$seed" push origin main
recorded=$(git -C "$seed" rev-parse HEAD)
stale=$(git -C %[1]s rev-parse HEAD)
git -C %[1]s fetch origin main
git -C %[1]s checkout main
test "$(git -C %[1]s rev-parse HEAD)" = "$stale"
git -C %[2]s update-index --cacheinfo 160000 "$recorded" projects/subproj
git -C %[2]s commit -m 'record newer submodule pointer'
printf 'recorded=%%s\nstale=%%s\n' "$recorded" "$stale"
`, projPath, campPath))
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "recorded":
			shas.recorded = strings.TrimSpace(value)
		case "stale":
			shas.stale = strings.TrimSpace(value)
		}
	}
	require.NotEmpty(t, shas.recorded)
	require.NotEmpty(t, shas.stale)
	require.NotEqual(t, shas.recorded, shas.stale)

	syncOutput, err := tc.RunCampInDir(campPath, "sync")
	require.Error(t, err, "sync must report stale branch drift instead of exiting cleanly")
	assert.Contains(t, syncOutput, "local main tip")
	assert.Contains(t, syncOutput, shas.stale[:8])
	assert.Contains(t, syncOutput, shas.recorded[:8])

	_, err = tc.RunCampInDir(campPath, "refs-sync")
	require.NoError(t, err, "refs-sync should not regress the recorded pointer after drift repair")

	recordedAfter := tc.GitOutput(t, campPath, "ls-tree", "HEAD", "--", "projects/subproj")
	assert.Contains(t, recordedAfter, shas.recorded, "recorded gitlink must remain at the newer commit")
	assert.NotContains(t, recordedAfter, shas.stale, "recorded gitlink must not regress to stale local main")

	subHead := tc.GitOutput(t, projPath, "rev-parse", "HEAD")
	assert.Equal(t, shas.recorded, subHead)
	_ = barePath
}
