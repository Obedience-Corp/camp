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

// intentContent returns a minimal intent markdown file with the given fields.
func intentContent(id, title, status string) string {
	return fmt.Sprintf(`---
id: %s
title: %s
type: feature
concept: ""
status: %s
created_at: "2026-03-03T12:00:00Z"
author: agent
priority: medium
horizon: later
promotion_criteria: "Ready when tests pass"
---

## Description

This is a test intent for promotion testing.
`, id, title, status)
}

// setupPromoteCampaign creates a campaign with the festivals directory structure
// and workflow/intents directories that promote needs.
func setupPromoteCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()

	path := "/campaigns/" + name
	_, err := tc.InitCampaign(path, name, "product")
	require.NoError(t, err)

	// Create festivals directory structure (camp init skips this if fest is unavailable)
	_, _, err = tc.ExecCommand("sh", "-c", fmt.Sprintf(
		"mkdir -p %s/festivals/planning %s/festivals/active %s/festivals/dungeon/completed",
		path, path, path))
	require.NoError(t, err)

	// Ensure intent status directories exist
	_, _, err = tc.ExecCommand("sh", "-c", fmt.Sprintf(
		"mkdir -p %s/workflow/intents/inbox %s/workflow/intents/active %s/workflow/intents/ready %s/workflow/intents/dungeon/done %s/workflow/intents/dungeon/killed",
		path, path, path, path, path))
	require.NoError(t, err)

	// Git commit the directory structure
	_, _, err = tc.ExecCommand("sh", "-c", fmt.Sprintf(
		"cd %s && git add . && git commit -m 'add festival and intent structure' --allow-empty", path))
	require.NoError(t, err)

	return path
}

// writeIntent creates an intent file in the given status directory.
func writeIntent(t *testing.T, tc *TestContainer, campaignPath, id, title, status string) {
	t.Helper()
	content := intentContent(id, title, status)
	filePath := fmt.Sprintf("%s/workflow/intents/%s/%s.md", campaignPath, status, id)
	err := tc.WriteFile(filePath, content)
	require.NoError(t, err)
}

// --- Promote Tests ---

func TestIntentPromote_CreatesFestival(t *testing.T) {
	if !festAvailable {
		t.Skip("fest CLI not available")
	}
	tc := GetSharedContainer(t)
	path := setupPromoteCampaign(t, tc, "promote-happy")

	intentID := "test-promote-20260303-120000"
	writeIntent(t, tc, path, intentID, "Test Promote Feature", "ready")

	// Promote the intent
	output, err := tc.RunCampInDir(path, "intent", "promote", intentID, "--no-commit", "--force")
	require.NoError(t, err, "promote should succeed, output: %s", output)

	// Verify intent moved to dungeon/done
	exists, err := tc.CheckFileExists(fmt.Sprintf("%s/workflow/intents/dungeon/done/%s.md", path, intentID))
	require.NoError(t, err)
	assert.True(t, exists, "intent should be in dungeon/done/")

	// Verify intent removed from ready
	exists, err = tc.CheckFileExists(fmt.Sprintf("%s/workflow/intents/ready/%s.md", path, intentID))
	require.NoError(t, err)
	assert.False(t, exists, "intent should no longer be in ready/")

	// Verify festival directory created in festivals/planning/
	planningFiles, err := tc.ListDirectory(path + "/festivals/planning")
	require.NoError(t, err)
	assert.NotEmpty(t, planningFiles, "should have files in festivals/planning/")

	// Find the festival directory name — it should contain the slug
	planningLS, _, err := tc.ExecCommand("sh", "-c", "ls "+path+"/festivals/planning/")
	require.NoError(t, err)
	dirs := strings.Split(strings.TrimSpace(planningLS), "\n")
	require.NotEmpty(t, dirs, "should have at least one festival directory")

	festivalDir := ""
	for _, d := range dirs {
		if strings.Contains(d, "test-promote") {
			festivalDir = d
			break
		}
	}
	require.NotEmpty(t, festivalDir, "festival directory should contain slug from intent title")

	// Verify festival has ingest directory with intent copy
	ingestPath := fmt.Sprintf("%s/festivals/planning/%s/001_INGEST/input_specs/%s.md",
		path, festivalDir, intentID)
	exists, err = tc.CheckFileExists(ingestPath)
	require.NoError(t, err)
	assert.True(t, exists, "intent should be copied to festival ingest directory")

	// Verify promoted_to is set in the intent frontmatter
	doneContent, err := tc.ReadFile(fmt.Sprintf("%s/workflow/intents/dungeon/done/%s.md", path, intentID))
	require.NoError(t, err)
	assert.Contains(t, doneContent, "promoted_to:", "promoted intent should have promoted_to field")
	assert.Contains(t, doneContent, festivalDir, "promoted_to should reference the festival directory")
}

func TestIntentPromote_NotReadyError(t *testing.T) {
	if !festAvailable {
		t.Skip("fest CLI not available")
	}
	tc := GetSharedContainer(t)
	path := setupPromoteCampaign(t, tc, "promote-notready")

	intentID := "inbox-intent-20260303-120000"
	writeIntent(t, tc, path, intentID, "Inbox Intent", "inbox")

	// Promote without --force should fail
	output, err := tc.RunCampInDir(path, "intent", "promote", intentID, "--no-commit")
	assert.Error(t, err, "promote should fail for non-ready intent")
	assert.Contains(t, strings.ToLower(output), "not ready",
		"error should mention intent is not ready")

	// Verify intent is still in inbox (not corrupted/moved)
	exists, err := tc.CheckFileExists(fmt.Sprintf("%s/workflow/intents/inbox/%s.md", path, intentID))
	require.NoError(t, err)
	assert.True(t, exists, "intent should still be in inbox/")
}

func TestIntentPromote_ForceFromInbox(t *testing.T) {
	if !festAvailable {
		t.Skip("fest CLI not available")
	}
	tc := GetSharedContainer(t)
	path := setupPromoteCampaign(t, tc, "promote-force")

	intentID := "force-promote-20260303-120000"
	writeIntent(t, tc, path, intentID, "Force Promote", "inbox")

	// Promote with --force should succeed
	output, err := tc.RunCampInDir(path, "intent", "promote", intentID, "--no-commit", "--force")
	require.NoError(t, err, "force promote should succeed, output: %s", output)

	// Verify intent moved to dungeon/done
	exists, err := tc.CheckFileExists(fmt.Sprintf("%s/workflow/intents/dungeon/done/%s.md", path, intentID))
	require.NoError(t, err)
	assert.True(t, exists, "intent should be in dungeon/done/")

	// Verify festival created
	planningLS, _, err := tc.ExecCommand("sh", "-c", "ls "+path+"/festivals/planning/")
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(planningLS), "should have a festival directory")
}

func TestIntentPromote_DryRun(t *testing.T) {
	if !festAvailable {
		t.Skip("fest CLI not available")
	}
	tc := GetSharedContainer(t)
	path := setupPromoteCampaign(t, tc, "promote-dryrun")

	intentID := "dryrun-intent-20260303-120000"
	writeIntent(t, tc, path, intentID, "Dry Run Intent", "ready")

	// Promote with --dry-run
	output, err := tc.RunCampInDir(path, "intent", "promote", intentID, "--dry-run")
	require.NoError(t, err, "dry-run should succeed, output: %s", output)
	assert.Contains(t, strings.ToLower(output), "dry run",
		"output should mention dry run")

	// Verify intent still in ready (unchanged)
	exists, err := tc.CheckFileExists(fmt.Sprintf("%s/workflow/intents/ready/%s.md", path, intentID))
	require.NoError(t, err)
	assert.True(t, exists, "intent should still be in ready/ after dry-run")

	// Verify no festival directory created
	planningLS, _, err := tc.ExecCommand("sh", "-c", "ls "+path+"/festivals/planning/ 2>/dev/null || echo EMPTY")
	require.NoError(t, err)
	trimmed := strings.TrimSpace(planningLS)
	assert.True(t, trimmed == "" || trimmed == "EMPTY",
		"no festival should be created during dry-run, got: %s", trimmed)
}

func TestIntentPromote_PreservesOtherIntents(t *testing.T) {
	if !festAvailable {
		t.Skip("fest CLI not available")
	}
	tc := GetSharedContainer(t)
	path := setupPromoteCampaign(t, tc, "promote-preserve")

	// Create 3 intents in different statuses
	readyID := "ready-intent-20260303-120000"
	activeID := "active-intent-20260303-120001"
	inboxID := "inbox-intent-20260303-120002"

	writeIntent(t, tc, path, readyID, "Ready Intent", "ready")
	writeIntent(t, tc, path, activeID, "Active Intent", "active")
	writeIntent(t, tc, path, inboxID, "Inbox Intent", "inbox")

	// Save original content of the other intents
	activeContent, err := tc.ReadFile(fmt.Sprintf("%s/workflow/intents/active/%s.md", path, activeID))
	require.NoError(t, err)
	inboxContent, err := tc.ReadFile(fmt.Sprintf("%s/workflow/intents/inbox/%s.md", path, inboxID))
	require.NoError(t, err)

	// Promote only the ready intent
	_, err = tc.RunCampInDir(path, "intent", "promote", readyID, "--no-commit", "--force")
	require.NoError(t, err)

	// Verify the other 2 intents are completely untouched
	activeAfter, err := tc.ReadFile(fmt.Sprintf("%s/workflow/intents/active/%s.md", path, activeID))
	require.NoError(t, err)
	assert.Equal(t, activeContent, activeAfter, "active intent should be unchanged")

	inboxAfter, err := tc.ReadFile(fmt.Sprintf("%s/workflow/intents/inbox/%s.md", path, inboxID))
	require.NoError(t, err)
	assert.Equal(t, inboxContent, inboxAfter, "inbox intent should be unchanged")

	// Verify both still exist in their original locations
	exists, err := tc.CheckFileExists(fmt.Sprintf("%s/workflow/intents/active/%s.md", path, activeID))
	require.NoError(t, err)
	assert.True(t, exists, "active intent should still be in active/")

	exists, err = tc.CheckFileExists(fmt.Sprintf("%s/workflow/intents/inbox/%s.md", path, inboxID))
	require.NoError(t, err)
	assert.True(t, exists, "inbox intent should still be in inbox/")
}

func TestIntentPromote_FestivalInPlanning(t *testing.T) {
	if !festAvailable {
		t.Skip("fest CLI not available")
	}
	tc := GetSharedContainer(t)
	path := setupPromoteCampaign(t, tc, "promote-planning")

	intentID := "planning-check-20260303-120000"
	writeIntent(t, tc, path, intentID, "Planning Check", "ready")

	// Promote
	_, err := tc.RunCampInDir(path, "intent", "promote", intentID, "--no-commit", "--force")
	require.NoError(t, err)

	// Verify festival is in festivals/planning/, NOT festivals/active/
	planningLS, _, err := tc.ExecCommand("sh", "-c", "ls "+path+"/festivals/planning/ 2>/dev/null || echo EMPTY")
	require.NoError(t, err)
	assert.NotEqual(t, "EMPTY", strings.TrimSpace(planningLS),
		"festival should exist in festivals/planning/")

	activeLS, _, err := tc.ExecCommand("sh", "-c", "ls "+path+"/festivals/active/ 2>/dev/null || echo EMPTY")
	require.NoError(t, err)
	trimmed := strings.TrimSpace(activeLS)
	assert.True(t, trimmed == "" || trimmed == "EMPTY",
		"festival should NOT be in festivals/active/, got: %s", trimmed)
}
