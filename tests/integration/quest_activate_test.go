//go:build integration

package integration

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var questIDPattern = regexp.MustCompile(`qst_[0-9]{8}_[a-z0-9]{6}`)

func setupActivationQuest(t *testing.T, path string) (*TestContainer, string) {
	t.Helper()
	tc := GetSharedContainer(t)
	_, err := tc.InitCampaign(path, "quest-activate", "product")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(path, "quest", "create", "billing",
		"--no-editor", "--purpose", "Q3 billing", "--no-commit")
	require.NoError(t, err, out)
	id := questIDPattern.FindString(out)
	require.NotEmpty(t, id, "could not parse quest id from: %s", out)
	return tc, id
}

func TestQuestUse_EmitsShellCodeForDialect(t *testing.T) {
	path := "/campaigns/quest-activate-use"
	tc, id := setupActivationQuest(t, path)

	posix, err := tc.RunCampInDir(path, "quest", "use", "billing")
	require.NoError(t, err, posix)
	assert.Contains(t, posix, "export CAMP_QUEST='"+id+"'", "posix export line")
	assert.Contains(t, posix, "Activated quest for this terminal", "human confirmation")

	fish, err := tc.RunCampInDir(path, "quest", "use", "billing", "--shell", "fish")
	require.NoError(t, err, fish)
	assert.Contains(t, fish, "set -gx CAMP_QUEST '"+id+"'", "fish set line")

	clear, err := tc.RunCampInDir(path, "quest", "clear")
	require.NoError(t, err, clear)
	assert.Contains(t, clear, "unset CAMP_QUEST")
}

func TestQuestStatus_ReflectsEnv(t *testing.T) {
	path := "/campaigns/quest-activate-status"
	tc, id := setupActivationQuest(t, path)

	// No env: not active.
	none, err := tc.RunCampInDir(path, "quest", "status")
	require.NoError(t, err, none)
	assert.Contains(t, none, "No terminal quest active")

	// Valid env: active + valid.
	valid, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && CAMP_QUEST="+id+" /camp quest status --json")
	require.NoError(t, err)
	assert.Contains(t, valid, "quest-status/v1alpha1")
	assert.Contains(t, valid, `"active": true`)
	assert.Contains(t, valid, `"valid": true`)
	assert.Contains(t, valid, id)

	// Bogus env: active but invalid.
	invalid, _, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && CAMP_QUEST=does-not-exist /camp quest status")
	require.NoError(t, err)
	assert.Contains(t, invalid, "Active quest is invalid")
	assert.Contains(t, invalid, "quest not found")
}
