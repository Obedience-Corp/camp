//go:build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectRemove_WorktreeDirectory(t *testing.T) {
	for _, scenario := range []struct {
		name                      string
		populated, dryRun, delete bool
	}{
		{name: "empty"},
		{name: "dry-run", dryRun: true},
		{name: "populated", populated: true},
		{name: "delete", populated: true, delete: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tc := GetSharedContainer(t)
			root, _, _ := setupRemoveSubmoduleCampaign(t, tc, "remove-wt-"+scenario.name)
			holder := root + "/projects/worktrees/subproj"
			tc.Shell(t, "mkdir -p "+holder)
			if scenario.populated {
				tc.Shell(t, fmt.Sprintf("printf 'preserve me' > %s/local.txt", holder))
			}
			args := []string{"p", "remove", "subproj", "--no-commit"}
			if scenario.dryRun {
				args = append(args, "--dry-run")
			}
			if scenario.delete {
				args = append(args, "--delete", "--force")
			}
			output, err := tc.RunCampInDir(root, args...)
			require.NoError(t, err)
			exists, err := tc.CheckDirExists(holder)
			require.NoError(t, err)
			assert.Equal(t, scenario.dryRun || (scenario.populated && !scenario.delete), exists)
			if scenario.dryRun {
				assert.Contains(t, output, "Delete worktrees")
				exists, err = tc.CheckDirExists(root + "/.git/modules/projects/subproj")
				require.NoError(t, err)
				assert.True(t, exists)
			} else if scenario.populated && !scenario.delete {
				assert.Contains(t, output, "retained")
				tc.Shell(t, "test \"$(cat "+holder+"/local.txt)\" = 'preserve me'")
			} else {
				assert.Contains(t, output, "Worktrees deleted")
			}
		})
	}
}
