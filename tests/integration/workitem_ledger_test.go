//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeWorkitemLedgerLines parses the campaign-wide workitem ledger's
// append-only JSONL content into one map per event, in append order.
func decodeWorkitemLedgerLines(t *testing.T, ledger string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(ledger), "\n")
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &e), "unmarshal ledger line: %s", line)
		events = append(events, e)
	}
	return events
}

// TestWorkitemLedger_CreateMovePromoteSharedTrail drives create -> dungeon
// move -> promote for a single directory-style bug workitem and asserts the
// campaign-wide workitem ledger records one event per lifecycle mutation,
// all three correlated by the same real workitem id (not the slug), through
// the shared ledger-append code path rather than per-command copies.
func TestWorkitemLedger_CreateMovePromoteSharedTrail(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "wledger-create-move-promote")

	output, err := tc.RunCampInDir(path,
		"workitem", "create", "flaky-test",
		"--type", "bug",
		"--title", "Flaky test",
		"--id", "bug-flaky-test-fixed",
	)
	require.NoError(t, err, "workitem create should succeed: %s", output)

	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'seed bug workitem'")
	require.NoError(t, err)

	ledger, err := tc.ReadFile(path + "/.campaign/workitems/.workitems.jsonl")
	require.NoError(t, err, "ledger should exist after create")
	events := decodeWorkitemLedgerLines(t, ledger)
	require.Len(t, events, 1, "expected exactly one ledger event after create: %s", ledger)
	assert.Equal(t, "create", events[0]["event"])
	assert.Equal(t, "bug-flaky-test-fixed", events[0]["id"])
	assert.Equal(t, "workflow/bug/flaky-test", events[0]["to"])

	// Move: general dungeon triage (no --workitem flag) is the same code
	// path that historically emitted no ledger event at all for the
	// bug/explore/design/audit workflows this intent targets.
	output, err = tc.RunCampInDir(path, "dungeon", "move", "flaky-test", "archived", "--workitem")
	require.NoError(t, err, "workitem dungeon move should succeed: %s", output)

	statusOutput, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(statusOutput), "git status should be clean after workitem dungeon move")

	findOutput, _, err := tc.ExecCommand(
		"find", path+"/workflow/bug/dungeon/archived",
		"-mindepth", "2", "-maxdepth", "2",
		"-name", "flaky-test", "-type", "d",
	)
	require.NoError(t, err)
	archivedDir := strings.TrimSpace(findOutput)
	require.NotEmpty(t, archivedDir, "should find the dated archived directory for flaky-test")

	ledger, err = tc.ReadFile(path + "/.campaign/workitems/.workitems.jsonl")
	require.NoError(t, err)
	events = decodeWorkitemLedgerLines(t, ledger)
	require.Len(t, events, 2, "expected create + move events: %s", ledger)
	moveEvent := events[1]
	assert.Equal(t, "move", moveEvent["event"])
	assert.Equal(t, "bug-flaky-test-fixed", moveEvent["id"], "move event must correlate with the create event by the real workitem id")
	assert.Equal(t, "workflow/bug/flaky-test", moveEvent["from"])
	assert.Contains(t, moveEvent["to"], "workflow/bug/dungeon/archived/")

	// Promote: from the item's current dungeon status location, matching
	// how camp workitem promote is used on an already-triaged item.
	output, err = tc.RunCampInDir(archivedDir, "workitem", "promote", "--target", "completed")
	require.NoError(t, err, "promote from dungeon status should succeed: %s", output)

	statusOutput, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(statusOutput), "git status should be clean after promote")

	ledger, err = tc.ReadFile(path + "/.campaign/workitems/.workitems.jsonl")
	require.NoError(t, err)
	events = decodeWorkitemLedgerLines(t, ledger)
	require.Len(t, events, 3, "expected create + move + promote events: %s", ledger)
	promoteEvent := events[2]
	assert.Equal(t, "promote", promoteEvent["event"])
	assert.Equal(t, "bug-flaky-test-fixed", promoteEvent["id"], "promote event must correlate with the create/move events by the real workitem id, not the slug")
	assert.Equal(t, "completed", promoteEvent["target"])
	assert.Contains(t, promoteEvent["to"], "workflow/bug/dungeon/completed/")

	for i, e := range events {
		assert.Equal(t, "bug-flaky-test-fixed", e["id"], "event %d should share the workitem id so the trail is derivable end to end", i)
	}
}

// TestWorkitemLedger_GeneralTriageMoveRecordsEvent covers the general (no
// --workitem flag) camp dungeon move for a workitem living directly under
// workflow/<type>/, the path a human/agent naturally uses when triaging
// from inside a bug/explore/design/audit workflow directory.
func TestWorkitemLedger_GeneralTriageMoveRecordsEvent(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "wledger-general-triage")

	output, err := tc.RunCampInDir(path,
		"workitem", "create", "stale-explore",
		"--type", "explore",
		"--title", "Stale exploration",
		"--id", "explore-stale-fixed",
	)
	require.NoError(t, err, "workitem create should succeed: %s", output)

	// The general (non --workitem) triage move resolves the nearest existing
	// "dungeon" directory by walking up from cwd; unlike the --workitem
	// flow it does not auto-initialize one, so the local per-type dungeon
	// needs to exist before triage runs from inside workflow/explore.
	_, err = tc.RunCampInDir(path+"/workflow/explore", "dungeon", "add")
	require.NoError(t, err, "local dungeon add should succeed")

	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'seed explore workitem'")
	require.NoError(t, err)

	output, err = tc.RunCampInDir(path+"/workflow/explore", "dungeon", "move", "stale-explore", "someday", "--triage")
	require.NoError(t, err, "general triage move should succeed: %s", output)

	statusOutput, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(statusOutput), "git status should be clean after the general triage move")

	ledger, err := tc.ReadFile(path + "/.campaign/workitems/.workitems.jsonl")
	require.NoError(t, err, "ledger should exist after the general triage move")
	events := decodeWorkitemLedgerLines(t, ledger)
	require.Len(t, events, 2, "expected create + move events: %s", ledger)
	moveEvent := events[1]
	assert.Equal(t, "move", moveEvent["event"])
	assert.Equal(t, "explore-stale-fixed", moveEvent["id"])
	assert.Equal(t, "workflow/explore/stale-explore", moveEvent["from"])
	assert.Contains(t, moveEvent["to"], "workflow/explore/dungeon/someday/")
}

// TestWorkitemLedger_ShelveAliasRecordsPromoteEvent covers the deprecated
// `camp shelve` alias, a second command entry point that shares the
// internal MoveToDungeon mutation with `camp workitem promote --target
// <status>` but historically emitted no ledger event of its own.
func TestWorkitemLedger_ShelveAliasRecordsPromoteEvent(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "wledger-shelve-alias")

	output, err := tc.RunCampInDir(path,
		"workitem", "create", "shelve-me",
		"--type", "design",
		"--title", "Shelve me",
		"--id", "design-shelve-me-fixed",
	)
	require.NoError(t, err, "workitem create should succeed: %s", output)
	_, _, err = tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'seed design workitem'")
	require.NoError(t, err)

	output, err = tc.RunCampInDir(path+"/workflow/design/shelve-me", "shelve", "archived")
	require.NoError(t, err, "shelve should succeed: %s", output)

	statusOutput, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(statusOutput), "git status should be clean after shelve")

	ledger, err := tc.ReadFile(path + "/.campaign/workitems/.workitems.jsonl")
	require.NoError(t, err, "ledger should exist after shelve")
	events := decodeWorkitemLedgerLines(t, ledger)
	require.Len(t, events, 2, "expected create + promote events: %s", ledger)
	shelveEvent := events[1]
	assert.Equal(t, "promote", shelveEvent["event"], "shelve is a promote alias and should record the same event type")
	assert.Equal(t, "design-shelve-me-fixed", shelveEvent["id"], "shelve event must correlate with the create event by the real workitem id, not the slug")
	assert.Equal(t, "archived", shelveEvent["target"])
	assert.Equal(t, "workflow/design/shelve-me", shelveEvent["from"])
	assert.Contains(t, shelveEvent["to"], "workflow/design/dungeon/archived/")
}

// TestWorkitemLedger_NonWorkitemTriageDoesNotPolluteLedger locks in that a
// plain (never adopted) triage file moved through the general dungeon move
// path is silently skipped rather than appended to the workitem ledger.
func TestWorkitemLedger_NonWorkitemTriageDoesNotPolluteLedger(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupDungeonCampaign(t, tc, "wledger-non-workitem")

	require.NoError(t, tc.WriteFile(path+"/dungeon/loose-note.md", "# Loose note\n"))
	_, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git add . && git commit -m 'add loose note'")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(path, "dungeon", "move", "loose-note.md", "archived")
	require.NoError(t, err, "move should succeed: %s", output)

	exists, err := tc.CheckFileExists(path + "/.campaign/workitems/.workitems.jsonl")
	require.NoError(t, err)
	assert.False(t, exists, "moving a non-workitem item must not create the workitem ledger")

	statusOutput, _, err := tc.ExecCommand("sh", "-c", "cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(statusOutput), "git status should be clean after the non-workitem move")
}
