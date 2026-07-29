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

// A commit-tree job whose first attempt died after update-ref must be
// recognized as landed on retry, even when the user has kept committing on top
// in the meantime. Regression for the review finding where every retry minted
// a new commit SHA, failed the expected-parent check, and parked work that was
// sitting in the log.
//
// Queue files are documented hand-editable, which is what lets this test hold
// a job still: it crafts the files directly instead of racing the worker a
// real enqueue would spawn. The retry jobs carry the same tree and parent the
// first job did, exactly as the surviving queue file of a crashed worker
// would.

func TestIntegration_CommitTreeRetryRecognizesLandedWork(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := "/campaigns/jobs-retry"
	_, err := tc.InitCampaign(campPath, "jobs-retry", "product")
	require.NoError(t, err)

	// Stage a change and capture it as a tree, exactly as -aw does. The tree
	// and parent are recorded once and shared by every crafted job below.
	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		printf 'queued content\n' > deferred.md
		git add deferred.md
		git write-tree > /tmp/jobs-retry-tree
		git rev-parse HEAD > /tmp/jobs-retry-parent
	`, campPath))

	craft := func(seq, attempts int, id string) {
		t.Helper()
		tc.Shell(t, fmt.Sprintf(`
			set -e
			cd %[1]s
			LANE=.campaign/cache/jobs/pending/%%2E
			mkdir -p "$LANE"
			cat > "$LANE/%[2]07d.json" <<JSON
{
  "id": "%[3]s",
  "seq": %[2]d,
  "kind": "commit-tree",
  "repo": ".",
  "tree": "$(cat /tmp/jobs-retry-tree)",
  "parent": "$(cat /tmp/jobs-retry-parent)",
  "message": "deferred: retry recognition",
  "created_at": "2026-07-29T00:00:00.000Z",
  "attempts": %[4]d
}
JSON
		`, campPath, seq, id, attempts))
	}

	craft(1, 0, "job-retry-first")
	_, err = tc.RunCampInDir(campPath, "jobs", "drain")
	require.NoError(t, err)

	subjects := tc.GitOutput(t, campPath, "log", "--format=%s")
	require.Equal(t, 1, strings.Count(subjects, "deferred: retry recognition"),
		"the first drain must land the queued commit")

	// The crash being simulated: the commit landed but the worker died before
	// unlinking the job, and the user kept working, burying the landed commit
	// under their own.
	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		printf 'user kept working\n' > user.md
		git add user.md
		git commit -q -m "user commit on top"
	`, campPath))

	// attempts 0 exercises the deep path: commit-tree runs, the expected-parent
	// ref move fails, and the first-parent walk recognizes the landed commit.
	craft(2, 0, "job-retry-deep")
	_, err = tc.RunCampInDir(campPath, "jobs", "drain")
	require.NoError(t, err)

	// attempts 1 exercises the reclaimed path: the walk short-circuits before
	// the message writer or commit-tree run at all.
	craft(3, 1, "job-retry-reclaimed")
	_, err = tc.RunCampInDir(campPath, "jobs", "drain")
	require.NoError(t, err)

	// Neither retry may park in failed/ or mint a second commit. HEAD must
	// still be the user's commit: recognizing landed work never moves refs.
	failed := tc.Shell(t, fmt.Sprintf(
		`ls %s/.campaign/cache/jobs/failed 2>/dev/null || true`, campPath))
	assert.Empty(t, strings.TrimSpace(failed), "a landed commit must not park its retry in failed/")

	subjects = tc.GitOutput(t, campPath, "log", "--format=%s")
	assert.Equal(t, 1, strings.Count(subjects, "deferred: retry recognition"),
		"retries of landed work must not create history")
	head := tc.GitOutput(t, campPath, "log", "-1", "--format=%s")
	assert.Equal(t, "user commit on top", strings.TrimSpace(head),
		"recognizing landed work must not move HEAD")
}
