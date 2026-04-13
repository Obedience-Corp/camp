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

// TestIntentAdd_TargetCampaignFlagForms verifies that all four valid
// invocation forms of the --campaign flag route the intent to the target
// campaign end-to-end through cobra. The normalizer is unit-tested in
// cmd/camp/args_normalize_test.go, but cobra's parsing pipeline is
// exercised here so a future pflag upgrade or cobra version bump that
// changes optional-value semantics will be caught.
//
// Forms covered:
//   --campaign <name>   long, space-separated  (the original bug)
//   --campaign=<name>   long, equals-attached
//   -c <name>           short, space-separated
//   -c=<name>           short, equals-attached
func TestIntentAdd_TargetCampaignFlagForms(t *testing.T) {
	tc := GetSharedContainer(t)

	const sourceCampaign = "/campaigns/intent-forms-source"
	_, err := tc.InitCampaign(sourceCampaign, "intent-forms-source", "product")
	require.NoError(t, err)

	cases := []struct {
		name         string
		targetSuffix string   // appended to form the unique target campaign name
		flagArgs     []string // arg tokens that go between `intent add` and the title
	}{
		{"long-space", "long-space", []string{"--campaign", "intent-forms-dest-long-space"}},
		{"long-equals", "long-equals", []string{"--campaign=intent-forms-dest-long-equals"}},
		{"short-space", "short-space", []string{"-c", "intent-forms-dest-short-space"}},
		{"short-equals", "short-equals", []string{"-c=intent-forms-dest-short-equals"}},
	}

	for _, tc2 := range cases {
		tc2 := tc2
		t.Run(tc2.name, func(t *testing.T) {
			targetName := "intent-forms-dest-" + tc2.targetSuffix
			targetPath := "/campaigns/" + targetName
			targetInbox := targetPath + "/.campaign/intents/inbox"

			_, err := tc.InitCampaign(targetPath, targetName, "product")
			require.NoError(t, err)

			title := fmt.Sprintf("Title for %s form", tc2.name)
			args := append([]string{"intent", "add"}, tc2.flagArgs...)
			args = append(args, title, "--no-commit")

			output, err := tc.RunCampInDir(sourceCampaign, args...)
			require.NoError(t, err, "camp intent add (%s form) should succeed", tc2.name)
			assert.Contains(t, output, targetInbox+"/",
				"intent should be created in target campaign %q (%s form)", targetName, tc2.name)

			targetLS, err := execLS(tc, targetInbox)
			require.NoError(t, err)
			targetFiles := strings.Fields(strings.TrimSpace(targetLS))
			assert.Len(t, targetFiles, 1,
				"target campaign %q should have exactly one intent after the %s invocation", targetName, tc2.name)
		})
	}

	// After all four forms, the source campaign's inbox must still be empty
	// (or not yet created). None of the forms should have leaked an intent
	// into the wrong campaign.
	sourceInbox := sourceCampaign + "/.campaign/intents/inbox"
	sourceInboxExists, err := tc.CheckDirExists(sourceInbox)
	require.NoError(t, err)
	if sourceInboxExists {
		sourceLS, err := execLS(tc, sourceInbox)
		require.NoError(t, err)
		assert.Empty(t, strings.TrimSpace(sourceLS), "source campaign inbox must remain empty across all forms")
	}
}
