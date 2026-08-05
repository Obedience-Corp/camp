//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type promoteJSON struct {
	ID                  string `json:"id"`
	Type                string `json:"type"`
	Target              string `json:"target"`
	From                string `json:"from"`
	To                  string `json:"to"`
	Committed           bool   `json:"committed"`
	ReleasedPriorityKey string `json:"released_priority_key"`
	ReleasedLinks       []struct {
		ID        string `json:"id"`
		ScopePath string `json:"scope_path"`
	} `json:"released_links"`
}

func runPromoteJSON(t *testing.T, tc *TestContainer, cwd string, args ...string) promoteJSON {
	t.Helper()
	out, err := tc.RunCampInDir(cwd, append([]string{"workitem", "promote", "--json"}, args...)...)
	require.NoError(t, err, "promote --json: %s", out)
	var p promoteJSON
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &p), "must parse: %s", out)
	return p
}

var datedResident = regexp.MustCompile(`^festivals/\.dungeon/[a-z]+/\d{4}-\d{2}-\d{2}/[a-z-]+$`)

// A resident completes into the festival-local dungeon, dated, for each status.
func TestRailExit_ResidentCompletesIntoFestivalDungeon(t *testing.T) {
	for _, status := range []string{"completed", "archived", "someday"} {
		t.Run(status, func(t *testing.T) {
			tc := GetSharedContainer(t)
			path := setupRailCampaign(t, tc, "exit-complete-"+status)
			railTo(t, tc, path+"/workflow/design/rail-feature", "active")

			res := runPromoteJSON(t, tc, path+"/festivals/active/rail-feature", "--target", status)
			assert.Equal(t, "festivals/active/rail-feature", res.From)
			assert.Regexp(t, datedResident, res.To, "must land in a dated festival-local bucket")
			assert.Contains(t, res.To, "festivals/.dungeon/"+status+"/")
			assert.True(t, res.Committed, "completion should auto-commit")

			exists, err := tc.CheckDirExists(path + "/" + res.To)
			require.NoError(t, err)
			assert.True(t, exists, "destination %s should exist", res.To)

			exists, err = tc.CheckDirExists(path + "/festivals/active/rail-feature")
			require.NoError(t, err)
			assert.False(t, exists, "resident should have left the stage")

			// It must not have gone to the workflow type's dungeon.
			strayed, _, err := tc.ExecCommand("sh", "-c",
				"find "+path+"/workflow/design/dungeon -type d -name rail-feature 2>/dev/null | head -1")
			require.NoError(t, err)
			assert.Empty(t, strings.TrimSpace(strayed), "resident must not land in the workflow dungeon")

			audit, err := tc.ReadFile(path + "/.campaign/workitems/.workitems.jsonl")
			require.NoError(t, err)
			assert.Contains(t, audit, res.To, "audit event should record the destination")

			status, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
			require.NoError(t, err)
			assert.Empty(t, strings.TrimSpace(status), "tree should be clean after the auto-commit")
		})
	}
}

// Shelving releases path-keyed state rather than re-homing it, because it ends
// the workitem's active life. The task text expected "follows into the dungeon";
// see results/resident-local-dungeon-completion.md for why release is correct.
func TestRailExit_CompletionReleasesPathState(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "exit-release-state")
	railTo(t, tc, path+"/workflow/design/rail-feature", "active")

	_, _, err := tc.ExecCommand("sh", "-c", "mkdir -p "+path+"/projects/worktrees/demo")
	require.NoError(t, err)
	out, err := tc.RunCampInDir(path, "workitem", "link", "design-rail-feature-fixed",
		"projects/worktrees/demo", "--role", "primary")
	require.NoError(t, err, "link: %s", out)
	out, err = tc.RunCampInDir(path, "workitem", "priority", "design-rail-feature-fixed", "high")
	require.NoError(t, err, "priority: %s", out)

	before, err := tc.ReadFile(path + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	require.Contains(t, before, "design:festivals/active/rail-feature")

	res := runPromoteJSON(t, tc, path+"/festivals/active/rail-feature", "--target", "completed")

	assert.Equal(t, "design:festivals/active/rail-feature", res.ReleasedPriorityKey,
		"the stranded priority key should be reported, not silently dropped")
	assert.NotEmpty(t, res.ReleasedLinks, "links should be released on shelve")

	links, err := tc.ReadFile(path + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.NotContains(t, links, "design:festivals/active/rail-feature")

	// Both stores are removed once empty; either absent or without the old key.
	prio, err := tc.ReadFile(path + "/.campaign/settings/workitems.json")
	if err == nil {
		assert.NotContains(t, prio, "design:festivals/active/rail-feature",
			"priority entry should not survive the shelve")
	}
}

func TestRailExit_DemoteReturnsHome(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "exit-demote")
	railTo(t, tc, path+"/workflow/design/rail-feature", "active")

	out, err := tc.RunCampInDir(path+"/festivals/active/rail-feature", "workitem", "demote", "--json")
	require.NoError(t, err, "demote: %s", out)
	var res promoteJSON
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &res), "must parse: %s", out)

	assert.Equal(t, "festivals/active/rail-feature", res.From)
	assert.Equal(t, "workflow/design/rail-feature", res.To)
	assert.Equal(t, "home", res.Target, "the event stream must distinguish a demote")
	assert.True(t, res.Committed)

	exists, err := tc.CheckFileExists(path + "/workflow/design/rail-feature/.workitem")
	require.NoError(t, err)
	assert.True(t, exists, "marker should travel home")

	exists, err = tc.CheckDirExists(path + "/festivals/active/rail-feature")
	require.NoError(t, err)
	assert.False(t, exists, "resident should have left the stage")

	gitStatus, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(gitStatus), "demote should auto-commit")
}

// Unlike a shelve, a demote re-homes path-keyed state, because the workitem stays
// active.
func TestRailExit_DemoteRehomesPathState(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "exit-demote-state")
	railTo(t, tc, path+"/workflow/design/rail-feature", "active")

	out, err := tc.RunCampInDir(path, "workitem", "priority", "design-rail-feature-fixed", "high")
	require.NoError(t, err, "priority: %s", out)

	out, err = tc.RunCampInDir(path+"/festivals/active/rail-feature", "workitem", "demote")
	require.NoError(t, err, "demote: %s", out)

	prio, err := tc.ReadFile(path + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	assert.Contains(t, prio, "design:workflow/design/rail-feature", "priority should follow it home")
	assert.NotContains(t, prio, "design:festivals/active/rail-feature", "old key must not survive")
}

func TestRailExit_DemoteRejections(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, tc *TestContainer, path string) string
		wantErr string
	}{
		{
			name: "from a workflow source",
			setup: func(t *testing.T, tc *TestContainer, path string) string {
				return path + "/workflow/design/rail-feature"
			},
			wantErr: "already at its type root",
		},
		{
			name: "from a dungeon",
			setup: func(t *testing.T, tc *TestContainer, path string) string {
				out, err := tc.RunCampInDir(path+"/workflow/design/rail-feature",
					"workitem", "promote", "--target", "completed")
				require.NoError(t, err, "shelve: %s", out)
				found, _, ferr := tc.ExecCommand("sh", "-c",
					"find "+path+"/workflow/design/dungeon -type d -name rail-feature | head -1")
				require.NoError(t, ferr)
				return strings.TrimSpace(found)
			},
			wantErr: "restoring a shelved workitem is not a demote",
		},
		{
			name: "destination occupied",
			setup: func(t *testing.T, tc *TestContainer, path string) string {
				railTo(t, tc, path+"/workflow/design/rail-feature", "ready")
				require.NoError(t, tc.WriteFile(path+"/workflow/design/rail-feature/README.md", "# Occupant\n"))
				return path + "/festivals/ready/rail-feature"
			},
			wantErr: "already exists",
		},
	}

	for _, tc2 := range tests {
		t.Run(tc2.name, func(t *testing.T) {
			tc := GetSharedContainer(t)
			path := setupRailCampaign(t, tc, "exit-reject-"+strings.ReplaceAll(tc2.name, " ", "-"))
			src := tc2.setup(t, tc, path)
			require.NotEmpty(t, src)

			_, stderr, exitCode, err := tc.RunCampSplitInDir(src, "workitem", "demote")
			require.NoError(t, err)
			assert.NotZero(t, exitCode, "a refused demote must exit non-zero")
			assert.Contains(t, stderr, tc2.wantErr)

			exists, cerr := tc.CheckDirExists(src)
			require.NoError(t, cerr)
			assert.True(t, exists, "a refused demote must leave the source in place")
		})
	}
}
