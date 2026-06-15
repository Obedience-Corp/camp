//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitRepairMigrationFailureDoesNotCommit(t *testing.T) {
	tc := GetSharedContainer(t)

	const campPath = "/campaigns/init-repair-migration-failure"
	_, err := tc.InitCampaign(campPath, "init-repair-migration-failure", "product")
	require.NoError(t, err)

	beforeHead := tc.GitOutput(t, campPath, "rev-parse", "HEAD")
	setupInitRepairMigrationConflict(t, tc, campPath)

	output, err := tc.RunCampInDir(campPath,
		"init", "--repair", "--yes", "--no-skills", "--no-register", "-m", "test mission")
	require.Error(t, err, "repair should exit non-zero when migration validation fails")
	assert.Contains(t, output, "Migration failed after 0 item(s) moved")
	assert.Contains(t, output, "migration pre-validation failed")

	afterHead := tc.GitOutput(t, campPath, "rev-parse", "HEAD")
	assert.Equal(t, beforeHead, afterHead, "repair must not auto-commit after migration failure")
}

func setupInitRepairMigrationConflict(t *testing.T, tc *TestContainer, campPath string) {
	t.Helper()

	dungeonCompletedDir := campPath + "/workflow/code_reviews/dungeon/completed"
	var conflictSetup strings.Builder
	for _, offset := range []int{-1, 0, 1} {
		conflictDir := statuspath.DatedDir(dungeonCompletedDir, time.Now().AddDate(0, 0, offset))
		fmt.Fprintf(&conflictSetup, "mkdir -p %s\n", conflictDir)
		fmt.Fprintf(&conflictSetup, "printf 'existing' > %s/blocked.md\n", conflictDir)
	}

	tc.Shell(t, fmt.Sprintf(`
set -e
rm -f %[1]s/AGENTS.md
mkdir -p %[1]s/workflow/code_reviews/completed
printf 'source' > %[1]s/workflow/code_reviews/completed/blocked.md
%[2]s
`, campPath, conflictSetup.String()))
}
