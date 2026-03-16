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

// setupPromoteCampaign creates a campaign with festivals and a legacy intent tree.
// Promotion should migrate legacy intent content into the canonical intent root.
func setupPromoteCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()

	path := "/campaigns/" + name
	_, err := tc.InitCampaign(path, name, "product")
	require.NoError(t, err)

	// Create festivals directory structure (camp init skips this if fest is unavailable).
	_, _, err = tc.ExecCommand("sh", "-c", fmt.Sprintf(
		"mkdir -p %s/festivals/.festival/templates %s/festivals/.festival/.state "+
			"%s/festivals/planning %s/festivals/active %s/festivals/dungeon/completed",
		path, path, path, path, path))
	require.NoError(t, err)

	// Seed a legacy intent tree so promote exercises migration compatibility.
	_, _, err = tc.ExecCommand("sh", "-c", fmt.Sprintf(
		"mkdir -p %s/workflow/intents/inbox %s/workflow/intents/active %s/workflow/intents/ready "+
			"%s/workflow/intents/dungeon/done %s/workflow/intents/dungeon/killed "+
			"%s/workflow/intents/dungeon/archived %s/workflow/intents/dungeon/someday",
		path, path, path, path, path, path, path))
	require.NoError(t, err)
	err = tc.WriteFile(path+"/workflow/intents/OBEY.md", "# legacy intent marker\n")
	require.NoError(t, err)

	// Git commit the directory structure.
	_, _, err = tc.ExecCommand("sh", "-c", fmt.Sprintf(
		"cd %s && git add . && git commit -m 'add festival and intent structure' --allow-empty", path))
	require.NoError(t, err)

	return path
}

// writeIntent creates an intent file in the legacy status directory.
func writeIntent(t *testing.T, tc *TestContainer, campaignPath, id, title, status string) {
	t.Helper()
	content := intentContent(id, title, status)
	filePath := fmt.Sprintf("%s/workflow/intents/%s/%s.md", campaignPath, status, id)
	err := tc.WriteFile(filePath, content)
	require.NoError(t, err)
}

// findPlanningFestivalDir finds a festival directory in festivals/planning that
// contains the expected slug.
func findPlanningFestivalDir(t *testing.T, tc *TestContainer, campaignPath, expectedSlug, promoteOutput string) string {
	t.Helper()
	planningLS, _, err := tc.ExecCommand("sh", "-c", "ls "+campaignPath+"/festivals/planning/")
	require.NoError(t, err)

	dirs := strings.Split(strings.TrimSpace(planningLS), "\n")
	for _, d := range dirs {
		if strings.Contains(d, expectedSlug) {
			return d
		}
	}
	allFestivals, _, _ := tc.ExecCommand("sh", "-c", "find "+campaignPath+"/festivals -maxdepth 2 -type d | sort")
	t.Fatalf("no festival directory contained slug %q; planning ls=%q; promote output=%q; festivals=%q",
		expectedSlug, planningLS, promoteOutput, allFestivals)
	return ""
}

func TestIntentPromote_MigratesLegacyIntentMarkerToCanonicalRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupPromoteCampaign(t, tc, "promote-marker-migration")

	intentID := "marker-migration-intent-20260303-120004"
	writeIntent(t, tc, path, intentID, "Marker Migration Intent", "ready")

	output, err := tc.RunCampInDir(path, "intent", "promote", intentID, "--target", "design", "--no-commit")
	require.NoError(t, err, "design promote should succeed, output: %s", output)

	canonicalObeyPath := fmt.Sprintf("%s/.campaign/intents/OBEY.md", path)
	canonicalObey, err := tc.ReadFile(canonicalObeyPath)
	require.NoError(t, err)
	assert.Equal(t, "# legacy intent marker\n", canonicalObey, "canonical marker should preserve migrated legacy content")

	legacyExists, err := tc.CheckFileExists(fmt.Sprintf("%s/workflow/intents/OBEY.md", path))
	require.NoError(t, err)
	assert.False(t, legacyExists, "legacy marker should be removed after command-path migration")
}

func TestIntentPromote_TargetFestival_NoExistingFestivals_CreatesFestivalAndActivatesIntent(t *testing.T) {
	if !festAvailable {
		t.Skip("fest CLI not available")
	}
	tc := GetSharedContainer(t)
	path := setupPromoteCampaign(t, tc, "promote-festival-first")

	intentID := "festival-first-intent-20260303-120000"
	writeIntent(t, tc, path, intentID, "Festival First Intent", "ready")

	output, err := tc.RunCampInDir(path, "intent", "promote", intentID, "--target", "festival", "--no-commit")
	require.NoError(t, err, "festival promote should succeed, output: %s", output)

	activePath := fmt.Sprintf("%s/.campaign/intents/active/%s.md", path, intentID)
	readyPath := fmt.Sprintf("%s/.campaign/intents/ready/%s.md", path, intentID)

	activeExists, err := tc.CheckFileExists(activePath)
	require.NoError(t, err)
	assert.True(t, activeExists, "intent should move to active/")

	readyExists, err := tc.CheckFileExists(readyPath)
	require.NoError(t, err)
	assert.False(t, readyExists, "intent should no longer be in ready/")

	festivalDir := findPlanningFestivalDir(t, tc, path, "festival-first-intent", output)
	ingestPath := fmt.Sprintf("%s/festivals/planning/%s/001_INGEST/input_specs/%s.md",
		path, festivalDir, intentID)
	ingestExists, err := tc.CheckFileExists(ingestPath)
	require.NoError(t, err)
	assert.True(t, ingestExists, "intent should be copied to festival ingest directory")

	activeContent, err := tc.ReadFile(activePath)
	require.NoError(t, err)
	assert.Contains(t, activeContent, "promoted_to:", "promoted intent should have promoted_to field")
	assert.Contains(t, activeContent, festivalDir, "promoted_to should reference the festival directory")
}

func TestIntentPromote_TargetDesign_TransactionalFailure_StaysReady(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupPromoteCampaign(t, tc, "promote-design-transactional")

	intentID := "design-transactional-failure-20260303-120001"
	writeIntent(t, tc, path, intentID, "Design Transactional Failure", "ready")

	// Block design directory creation by placing a regular file at the exact path
	// createDesignDoc will try to create as a directory.
	_, code, err := tc.ExecCommand("sh", "-c", fmt.Sprintf(
		"mkdir -p %s/workflow/design && printf 'blocked' > %s/workflow/design/design-transactional-failure-20260303-120001",
		path, path))
	require.NoError(t, err)
	require.Equal(t, 0, code, "failed to create blocker file")

	output, err := tc.RunCampInDir(path, "intent", "promote", intentID, "--target", "design", "--no-commit")
	require.Error(t, err, "design promote should fail when workflow/design is blocked")
	assert.Contains(t, strings.ToLower(output), "failed to create design doc")

	readyPath := fmt.Sprintf("%s/.campaign/intents/ready/%s.md", path, intentID)
	activePath := fmt.Sprintf("%s/.campaign/intents/active/%s.md", path, intentID)

	readyExists, err := tc.CheckFileExists(readyPath)
	require.NoError(t, err)
	assert.True(t, readyExists, "intent should remain in ready/ on design failure")

	activeExists, err := tc.CheckFileExists(activePath)
	require.NoError(t, err)
	assert.False(t, activeExists, "intent should not move to active/ on design failure")
}

func TestIntentPromote_TargetFestivalThenDesign_BothArtifactsCreated(t *testing.T) {
	if !festAvailable {
		t.Skip("fest CLI not available")
	}
	tc := GetSharedContainer(t)
	path := setupPromoteCampaign(t, tc, "promote-festival-then-design")

	festivalID := "festival-target-one-20260303-120002"
	designID := "second-design-target-20260303-120003"
	writeIntent(t, tc, path, festivalID, "Festival Target One", "ready")
	writeIntent(t, tc, path, designID, "Second Design Target", "ready")

	// Promote first intent to festival (no existing festivals in planning yet).
	festivalOut, err := tc.RunCampInDir(path, "intent", "promote", festivalID, "--target", "festival", "--no-commit")
	require.NoError(t, err, "festival promote should succeed, output: %s", festivalOut)

	festivalDir := findPlanningFestivalDir(t, tc, path, "festival-target-one", festivalOut)
	festivalPath := fmt.Sprintf("%s/festivals/planning/%s", path, festivalDir)
	festivalExists, err := tc.CheckDirExists(festivalPath)
	require.NoError(t, err)
	assert.True(t, festivalExists, "festival directory should exist")

	// Promote second intent to design in the same campaign.
	designOut, err := tc.RunCampInDir(path, "intent", "promote", designID, "--target", "design", "--no-commit")
	require.NoError(t, err, "design promote should succeed, output: %s", designOut)

	designDir := fmt.Sprintf("workflow/design/second-design-target-20260303-120003")
	designReadme := fmt.Sprintf("%s/%s/README.md", path, designDir)
	designReadmeExists, err := tc.CheckFileExists(designReadme)
	require.NoError(t, err)
	assert.True(t, designReadmeExists, "design README should be created")

	// Verify both promoted intents are active with correct promoted_to values.
	festivalActivePath := fmt.Sprintf("%s/.campaign/intents/active/%s.md", path, festivalID)
	designActivePath := fmt.Sprintf("%s/.campaign/intents/active/%s.md", path, designID)

	festivalActiveContent, err := tc.ReadFile(festivalActivePath)
	require.NoError(t, err)
	assert.Contains(t, festivalActiveContent, "promoted_to:")
	assert.Contains(t, festivalActiveContent, festivalDir)

	designActiveContent, err := tc.ReadFile(designActivePath)
	require.NoError(t, err)
	assert.Contains(t, designActiveContent, "promoted_to:")
	assert.Contains(t, designActiveContent, designDir)
}
