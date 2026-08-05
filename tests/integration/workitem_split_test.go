//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// markerOf reads a workitem's .workitem marker.
func markerOf(t *testing.T, tc *TestContainer, path, rel string) string {
	t.Helper()
	raw, err := tc.ReadFile(path + "/" + rel + "/.workitem")
	require.NoError(t, err)
	return raw
}

// commitAll stages and commits inside the container fixture. Split auto-commits
// on its own; this is only for arranging preconditions the test needs.
func commitAll(t *testing.T, tc *TestContainer, path, message string) {
	t.Helper()
	tc.Shell(t, fmt.Sprintf("cd %s && git add -A && git %s -q -m %q", path, "commit", message))
}

// TestWorkitemSplit_CreatesSuccessorAndStampsBothWays is doc 06's worked
// example: the successor exists, both markers are stamped, one commit.
func TestWorkitemSplit_CreatesSuccessorAndStampsBothWays(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "wi-split-create", 2, 0)

	before := tc.Shell(t, "cd "+path+" && git rev-list --count HEAD")

	output, err := tc.RunCampInDir(path, "workitem", "split", "design-item-1",
		"--into", "fest-festival-ingest", "--json")
	require.NoError(t, err, output)

	var result struct {
		Parent struct {
			StableID string `json:"stable_id"`
		} `json:"parent"`
		Successors []struct {
			Name         string `json:"name"`
			StableID     string `json:"stable_id"`
			Ref          string `json:"ref"`
			Type         string `json:"type"`
			RelativePath string `json:"relative_path"`
			Created      bool   `json:"created"`
		} `json:"successors"`
		Gate struct {
			Blocked bool     `json:"blocked"`
			Missing []string `json:"missing"`
		} `json:"gate"`
		Committed bool `json:"committed"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &result))

	require.Len(t, result.Successors, 1)
	successor := result.Successors[0]
	assert.True(t, successor.Created)
	assert.NotEmpty(t, successor.StableID, "a created successor has a real id")
	assert.NotEmpty(t, successor.Ref)
	assert.Equal(t, "design", successor.Type, "type defaults to the parent's")
	assert.True(t, result.Committed)
	assert.False(t, result.Gate.Blocked, "the successor it just made exists")

	// The successor exists with its seeded README.
	readme, err := tc.ReadFile(path + "/" + successor.RelativePath + "/README.md")
	require.NoError(t, err)
	assert.Contains(t, readme, "Split from")
	assert.Contains(t, readme, "## Scope carried from parent")

	// Lineage both ways.
	parentMarker := markerOf(t, tc, path, "workflow/design/design-item-1")
	assert.Contains(t, parentMarker, "split_into:")
	assert.Contains(t, parentMarker, successor.StableID)
	assert.Contains(t, parentMarker, "split_at:")

	successorMarker := markerOf(t, tc, path, successor.RelativePath)
	assert.Contains(t, successorMarker, "split_from: design-item-1")

	// One commit covering all of it.
	after := tc.Shell(t, "cd "+path+" && git rev-list --count HEAD")
	assert.Equal(t, 1, countDelta(t, before, after), "a split lands as exactly one commit")

	status := tc.Shell(t, "cd "+path+" && git status --porcelain")
	assert.Empty(t, strings.TrimSpace(status), "the split leaves no uncommitted residue")
}

// TestWorkitemSplit_AdoptsAnExistingWorkitem covers the --adopt path.
func TestWorkitemSplit_AdoptsAnExistingWorkitem(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "wi-split-adopt", 2, 0)

	output, err := tc.RunCampInDir(path, "workitem", "split", "design-item-1",
		"--adopt", "workflow/design/design-item-2", "--json")
	require.NoError(t, err, output)

	var result struct {
		Successors []struct {
			StableID string `json:"stable_id"`
			Created  bool   `json:"created"`
			Adopted  bool   `json:"adopted"`
		} `json:"successors"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &result))
	require.Len(t, result.Successors, 1)
	assert.False(t, result.Successors[0].Created, "an existing workitem is declared, not created")
	assert.False(t, result.Successors[0].Adopted,
		"it was already a workitem, so its id is recorded rather than reassigned")
	assert.Equal(t, "design-item-2", result.Successors[0].StableID)

	assert.Contains(t, markerOf(t, tc, path, "workflow/design/design-item-2"),
		"split_from: design-item-1")
}

// TestWorkitemSplit_MixedCreateAndAdopt is the shape doc 06's worked example
// uses.
func TestWorkitemSplit_MixedCreateAndAdopt(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "wi-split-mixed", 3, 0)

	output, err := tc.RunCampInDir(path, "workitem", "split", "design-item-1",
		"--into", "fest-festival-ingest",
		"--adopt", "workflow/design/design-item-2", "--json")
	require.NoError(t, err, output)

	var result struct {
		Successors []struct {
			StableID string `json:"stable_id"`
			Created  bool   `json:"created"`
		} `json:"successors"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &result))
	require.Len(t, result.Successors, 2)

	parentMarker := markerOf(t, tc, path, "workflow/design/design-item-1")
	for _, successor := range result.Successors {
		assert.Contains(t, parentMarker, successor.StableID,
			"every successor is listed on the parent")
	}
}

// TestWorkitemSplit_GateBlocksTerminalPromotion is D2 made mechanical.
func TestWorkitemSplit_GateBlocksTerminalPromotion(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "wi-split-gate", 2, 0)

	output, err := tc.RunCampInDir(path, "workitem", "split", "design-item-1",
		"--into", "fest-festival-ingest", "--json")
	require.NoError(t, err, output)

	var result struct {
		Successors []struct {
			StableID     string `json:"stable_id"`
			RelativePath string `json:"relative_path"`
		} `json:"successors"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &result))
	require.Len(t, result.Successors, 1)
	successor := result.Successors[0]

	// While the successor exists the parent retires normally.
	output, err = tc.RunCampInDir(path, "workitem", "promote", "design-item-1",
		"--target", "completed", "--dry-run")
	require.NoError(t, err, output)

	// Remove it, and the gate refuses — naming what is missing.
	tc.Shell(t, "cd "+path+" && rm -rf "+successor.RelativePath)
	commitAll(t, tc, path, "drop successor")

	output, err = tc.RunCampInDir(path, "workitem", "promote", "design-item-1", "--target", "completed")
	require.Error(t, err, "a parent with a missing successor must not retire")
	assert.Contains(t, output, successor.StableID, "the refusal names what is missing")
	assert.Contains(t, output, "--force")

	// A dry-run reports the block too, rather than promising a promote that
	// would fail.
	output, err = tc.RunCampInDir(path, "workitem", "promote", "design-item-1",
		"--target", "completed", "--dry-run")
	require.Error(t, err, "a dry-run must not claim it would promote")
	assert.Contains(t, output, successor.StableID)

	// --force is the documented escape.
	output, err = tc.RunCampInDir(path, "workitem", "promote", "design-item-1",
		"--target", "completed", "--force")
	require.NoError(t, err, output)
	assert.Contains(t, output, "dungeon/completed")
}

// TestWorkitemSplit_GateIgnoresNonTerminalTargets: the gate guards retirement,
// not ordinary rail movement.
func TestWorkitemSplit_GateIgnoresNonTerminalTargets(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "wi-split-gate-rail", 2, 0)

	output, err := tc.RunCampInDir(path, "workitem", "split", "design-item-1",
		"--into", "fest-festival-ingest", "--json")
	require.NoError(t, err, output)
	tc.Shell(t, "cd "+path+" && rm -rf workflow/design/fest-festival-ingest")
	commitAll(t, tc, path, "drop successor")

	output, err = tc.RunCampInDir(path, "workitem", "promote", "design-item-1",
		"--target", "ready", "--dry-run")
	require.NoError(t, err, output)
}

// TestWorkitemSplit_DryRunChangesNothing covers the read path.
func TestWorkitemSplit_DryRunChangesNothing(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "wi-split-dryrun", 2, 0)

	output, err := tc.RunCampInDir(path, "workitem", "split", "design-item-1",
		"--into", "fest-festival-ingest", "--dry-run")
	require.NoError(t, err, output)
	assert.Contains(t, output, "Nothing was changed")
	assert.Contains(t, output, "retirement gate")

	assert.NotContains(t, markerOf(t, tc, path, "workflow/design/design-item-1"), "split_into")
	_, err = tc.ReadFile(path + "/workflow/design/fest-festival-ingest/.workitem")
	require.Error(t, err, "a dry-run creates nothing")

	// The dry-run does not invent ids it has not generated.
	output, err = tc.RunCampInDir(path, "workitem", "split", "design-item-1",
		"--into", "fest-festival-ingest", "--dry-run", "--json")
	require.NoError(t, err, output)
	var result struct {
		Successors []struct {
			Name     string `json:"name"`
			StableID string `json:"stable_id"`
		} `json:"successors"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &result))
	require.Len(t, result.Successors, 1)
	assert.Equal(t, "fest-festival-ingest", result.Successors[0].Name)
	assert.Empty(t, result.Successors[0].StableID,
		"an id is assigned at creation; previewing one would consume it")
}

// TestWorkitemSplit_RejectsBadInput covers the guards that run before any write.
func TestWorkitemSplit_RejectsBadInput(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "wi-split-bad", 2, 0)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no successors", args: []string{"design-item-1"}, want: "at least one successor"},
		{
			name: "duplicate successor",
			args: []string{"design-item-1", "--into", "dup", "--into", "dup"},
			want: "named more than once",
		},
		{
			name: "parent as its own successor",
			args: []string{"design-item-1", "--into", "design-item-1"},
			want: "cannot be its own successor",
		},
		{
			name: "adopting a path that does not exist",
			args: []string{"design-item-1", "--adopt", "workflow/design/nope"},
			want: "no such path",
		},
		{name: "unknown parent", args: []string{"nope", "--into", "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := tc.RunCampInDir(path, append([]string{"workitem", "split"}, tt.args...)...)
			require.Error(t, err, output)
			if tt.want != "" {
				assert.Contains(t, output, tt.want)
			}
			// Nothing was written on the way to the refusal.
			assert.NotContains(t, markerOf(t, tc, path, "workflow/design/design-item-1"), "split_into")
		})
	}
}

// TestWorkitemSplit_ReStampIsIdempotent: apply may retry a row, so a second
// split must not append a duplicate key.
func TestWorkitemSplit_ReStampIsIdempotent(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "wi-split-idempotent", 2, 0)

	output, err := tc.RunCampInDir(path, "workitem", "split", "design-item-1",
		"--adopt", "workflow/design/design-item-2", "--json")
	require.NoError(t, err, output)
	first := markerOf(t, tc, path, "workflow/design/design-item-1")

	output, err = tc.RunCampInDir(path, "workitem", "split", "design-item-1",
		"--adopt", "workflow/design/design-item-2", "--json")
	require.NoError(t, err, output)
	second := markerOf(t, tc, path, "workflow/design/design-item-1")

	assert.Equal(t, 1, strings.Count(second, "split_into:"),
		"re-stamping replaces the key in place rather than appending a second")
	assert.Equal(t, strings.Count(first, "design-item-2"), strings.Count(second, "design-item-2"),
		"the successor is listed once, not twice")
}

// TestWorkitemSplit_LeavesTheWorkitemJSONContractAlone is the public-surface
// guard: split adds marker fields, and no existing consumer may see a change.
func TestWorkitemSplit_LeavesTheWorkitemJSONContractAlone(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "wi-split-contract", 2, 0)

	before, err := tc.RunCampInDir(path, "workitem", "--json", "--show-parked")
	require.NoError(t, err, before)

	output, err := tc.RunCampInDir(path, "workitem", "split", "design-item-1",
		"--adopt", "workflow/design/design-item-2", "--json")
	require.NoError(t, err, output)

	after, err := tc.RunCampInDir(path, "workitem", "--json", "--show-parked")
	require.NoError(t, err, after)

	assert.Equal(t, itemKeySet(t, before), itemKeySet(t, after),
		"a split must not add or remove keys in the workitem list contract")
}

// itemKeySet returns the sorted union of item keys in a workitem listing.
func itemKeySet(t *testing.T, output string) []string {
	t.Helper()
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractJSON(t, output)), &payload))

	seen := map[string]bool{}
	for _, item := range payload.Items {
		for key := range item {
			seen[key] = true
		}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sortStrings(out)
	return out
}

// sortStrings sorts in place.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// countDelta parses two `git rev-list --count` outputs and returns the change.
func countDelta(t *testing.T, before, after string) int {
	t.Helper()
	return atoiTrim(t, after) - atoiTrim(t, before)
}

func atoiTrim(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, r := range strings.TrimSpace(s) {
		require.True(t, r >= '0' && r <= '9', "not a number: %q", s)
		n = n*10 + int(r-'0')
	}
	return n
}
