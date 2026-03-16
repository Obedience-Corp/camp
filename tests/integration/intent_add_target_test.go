//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntentAdd_TargetCampaignWritesToSelectedRoot(t *testing.T) {
	tc := GetSharedContainer(t)

	_, err := tc.InitCampaign("/campaigns/intent-target-current", "intent-target-current", "product")
	require.NoError(t, err)
	_, err = tc.InitCampaign("/campaigns/intent-target-dest", "intent-target-dest", "product")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(
		"/campaigns/intent-target-current",
		"intent", "add",
		"--campaign", "intent-target-dest",
		"Targeted Intent",
		"--no-commit",
	)
	require.NoError(t, err)
	assert.Contains(t, output, "/campaigns/intent-target-dest/.campaign/intents/inbox/", "intent should be created in the target campaign")

	targetLS, err := execLS(tc, "/campaigns/intent-target-dest/.campaign/intents/inbox")
	require.NoError(t, err)
	targetFiles := strings.Fields(strings.TrimSpace(targetLS))
	require.Len(t, targetFiles, 1, "target campaign should receive exactly one intent")

	currentInboxExists, err := tc.CheckDirExists("/campaigns/intent-target-current/.campaign/intents/inbox")
	require.NoError(t, err)
	if !currentInboxExists {
		return
	}

	currentLS, err := execLS(tc, "/campaigns/intent-target-current/.campaign/intents/inbox")
	require.NoError(t, err)
	assert.True(t, strings.TrimSpace(currentLS) == "", "current campaign inbox should remain empty")
}
