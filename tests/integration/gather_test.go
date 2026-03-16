//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execLS is a helper to run ls and return output
func execLS(tc *TestContainer, path string) (string, error) {
	output, exitCode, err := tc.ExecCommand("ls", path)
	if err != nil {
		return "", err
	}
	if exitCode != 0 {
		return output, nil // Return output even on non-zero exit
	}
	return output, nil
}

func TestIntentGather_ByIDs(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/gather-ids", "gather-ids", "product")
	require.NoError(t, err)

	// Create test intents
	_, err = tc.RunCampInDir("/campaigns/gather-ids", "intent", "add", "Auth Feature")
	require.NoError(t, err)
	_, err = tc.RunCampInDir("/campaigns/gather-ids", "intent", "add", "Login System")
	require.NoError(t, err)

	// List intents to get IDs
	listOutput, err := tc.RunCampInDir("/campaigns/gather-ids", "intent", "list")
	require.NoError(t, err)
	t.Logf("Intent list: %s", listOutput)

	// Get intent IDs from inbox directory
	lsOutput, err := execLS(tc, "/campaigns/gather-ids/.campaign/intents/inbox")
	require.NoError(t, err)

	// Parse out intent IDs (remove .md extension)
	files := strings.Split(strings.TrimSpace(lsOutput), "\n")
	require.GreaterOrEqual(t, len(files), 2, "should have at least 2 intents")

	id1 := strings.TrimSuffix(files[0], ".md")
	id2 := strings.TrimSuffix(files[1], ".md")

	// Gather the intents
	output, err := tc.RunCampInDir("/campaigns/gather-ids", "intent", "gather", id1, id2, "--title", "Unified Auth")
	require.NoError(t, err, "gather should succeed")
	assert.Contains(t, output, "Gathered 2 intents", "output should confirm gathering")
	assert.Contains(t, output, "Archived 2", "output should confirm archiving")
}

func TestIntentGather_ByTag(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/gather-tag", "gather-tag", "product")
	require.NoError(t, err)

	// Create test intents with tags (write directly to include tags)
	intent1 := `---
id: 20260129-auth1
title: Auth Feature One
status: inbox
created_at: 2026-01-29
tags:
  - auth
  - security
---

# Auth Feature One

First auth feature.
`
	intent2 := `---
id: 20260129-auth2
title: Auth Feature Two
status: inbox
created_at: 2026-01-29
tags:
  - auth
  - login
---

# Auth Feature Two

Second auth feature.
`
	intent3 := `---
id: 20260129-unrelated
title: Unrelated Feature
status: inbox
created_at: 2026-01-29
tags:
  - ui
---

# Unrelated Feature

Not related to auth.
`
	_, _, err = tc.ExecCommand("mkdir", "-p", "/campaigns/gather-tag/workflow/intents/inbox")
	require.NoError(t, err)
	err = tc.WriteFile("/campaigns/gather-tag/workflow/intents/inbox/20260129-auth1.md", intent1)
	require.NoError(t, err)
	err = tc.WriteFile("/campaigns/gather-tag/workflow/intents/inbox/20260129-auth2.md", intent2)
	require.NoError(t, err)
	err = tc.WriteFile("/campaigns/gather-tag/workflow/intents/inbox/20260129-unrelated.md", intent3)
	require.NoError(t, err)

	// Gather by auth tag
	output, err := tc.RunCampInDir("/campaigns/gather-tag", "intent", "gather", "--tag", "auth", "--title", "Combined Auth")
	require.NoError(t, err, "gather by tag should succeed")
	assert.Contains(t, output, "Gathered 2 intents", "should gather exactly 2 auth-tagged intents")
}

func TestIntentGather_DryRun(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/gather-dry", "gather-dry", "product")
	require.NoError(t, err)

	// Create test intents
	_, err = tc.RunCampInDir("/campaigns/gather-dry", "intent", "add", "Feature A")
	require.NoError(t, err)
	_, err = tc.RunCampInDir("/campaigns/gather-dry", "intent", "add", "Feature B")
	require.NoError(t, err)

	// Get intent IDs
	lsOutput, err := execLS(tc, "/campaigns/gather-dry/.campaign/intents/inbox")
	require.NoError(t, err)
	files := strings.Split(strings.TrimSpace(lsOutput), "\n")
	require.GreaterOrEqual(t, len(files), 2)

	id1 := strings.TrimSuffix(files[0], ".md")
	id2 := strings.TrimSuffix(files[1], ".md")

	// Dry run gather
	output, err := tc.RunCampInDir("/campaigns/gather-dry", "intent", "gather", id1, id2, "--title", "Combined", "--dry-run")
	require.NoError(t, err, "dry run should succeed")
	assert.Contains(t, output, "Would gather 2 intents", "output should indicate dry run")
	assert.Contains(t, output, "Source intents", "output should list sources")

	// Verify no actual changes (intents still in inbox)
	lsAfter, err := execLS(tc, "/campaigns/gather-dry/.campaign/intents/inbox")
	require.NoError(t, err)
	assert.Equal(t, lsOutput, lsAfter, "inbox should be unchanged after dry run")
}

func TestIntentGather_NoArchive(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/gather-noarch", "gather-noarch", "product")
	require.NoError(t, err)

	// Create test intents
	_, err = tc.RunCampInDir("/campaigns/gather-noarch", "intent", "add", "Feature X")
	require.NoError(t, err)
	_, err = tc.RunCampInDir("/campaigns/gather-noarch", "intent", "add", "Feature Y")
	require.NoError(t, err)

	// Get intent IDs
	lsOutput, err := execLS(tc, "/campaigns/gather-noarch/.campaign/intents/inbox")
	require.NoError(t, err)
	files := strings.Split(strings.TrimSpace(lsOutput), "\n")
	require.GreaterOrEqual(t, len(files), 2)

	id1 := strings.TrimSuffix(files[0], ".md")
	id2 := strings.TrimSuffix(files[1], ".md")

	// Gather without archiving
	output, err := tc.RunCampInDir("/campaigns/gather-noarch", "intent", "gather", id1, id2, "--title", "Combined", "--no-archive")
	require.NoError(t, err, "gather without archiving should succeed")
	assert.Contains(t, output, "Gathered 2 intents", "should confirm gathering")
	assert.NotContains(t, output, "Archived", "should not mention archiving")

	// Verify source intents still exist in inbox
	exists1, err := tc.CheckFileExists("/campaigns/gather-noarch/.campaign/intents/inbox/" + id1 + ".md")
	require.NoError(t, err)
	assert.True(t, exists1, "source intent 1 should still exist")

	exists2, err := tc.CheckFileExists("/campaigns/gather-noarch/.campaign/intents/inbox/" + id2 + ".md")
	require.NoError(t, err)
	assert.True(t, exists2, "source intent 2 should still exist")
}

func TestIntentGather_ErrorTooFewIntents(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/gather-err", "gather-err", "product")
	require.NoError(t, err)

	// Create only one intent
	_, err = tc.RunCampInDir("/campaigns/gather-err", "intent", "add", "Single Intent")
	require.NoError(t, err)

	// Get intent ID
	lsOutput, err := execLS(tc, "/campaigns/gather-err/.campaign/intents/inbox")
	require.NoError(t, err)
	files := strings.Split(strings.TrimSpace(lsOutput), "\n")
	require.Equal(t, 1, len(files))

	id1 := strings.TrimSuffix(files[0], ".md")

	// Try to gather with only one intent
	output, err := tc.RunCampInDir("/campaigns/gather-err", "intent", "gather", id1, "--title", "Failed")
	assert.Error(t, err, "gather should fail with only 1 intent")
	assert.Contains(t, output, "at least 2", "error should mention minimum requirement")
}

func TestIntentGather_ErrorNoTitle(t *testing.T) {
	tc := GetSharedContainer(t)

	// Setup: Create campaign
	_, err := tc.InitCampaign("/campaigns/gather-notitle", "gather-notitle", "product")
	require.NoError(t, err)

	// Create test intents
	_, err = tc.RunCampInDir("/campaigns/gather-notitle", "intent", "add", "Intent A")
	require.NoError(t, err)
	_, err = tc.RunCampInDir("/campaigns/gather-notitle", "intent", "add", "Intent B")
	require.NoError(t, err)

	// Get intent IDs
	lsOutput, err := execLS(tc, "/campaigns/gather-notitle/.campaign/intents/inbox")
	require.NoError(t, err)
	files := strings.Split(strings.TrimSpace(lsOutput), "\n")

	id1 := strings.TrimSuffix(files[0], ".md")
	id2 := strings.TrimSuffix(files[1], ".md")

	// Try to gather without title
	output, err := tc.RunCampInDir("/campaigns/gather-notitle", "intent", "gather", id1, id2)
	assert.Error(t, err, "gather should fail without title")
	assert.Contains(t, output, "title is required", "error should mention title requirement")
}
