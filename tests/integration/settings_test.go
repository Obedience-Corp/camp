//go:build integration
// +build integration

package integration

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipUnlessSettingsTTYTests gates the camp settings TUI integration tests.
// They exercise the real write paths end to end and pass reliably run in
// isolation, but are flaky under concurrent full-suite load because of camp's
// tracked bubbletea init tty-query stall: termenv blocks up to 5s per program
// when the pty does not answer the OSC 11 / DSR query fast enough under CPU
// pressure (intent: camp-same-bubbletea-init-tty-*). Opt in with
// CAMP_SETTINGS_TTY_TESTS=1 to verify the write paths.
func skipUnlessSettingsTTYTests(t *testing.T) {
	t.Helper()
	if os.Getenv("CAMP_SETTINGS_TTY_TESTS") == "" {
		t.Skip("set CAMP_SETTINGS_TTY_TESTS=1 to run camp settings TUI tests (tracked bubbletea init tty-query stall makes them flaky under load)")
	}
}

// Navigation for the catalog-driven settings menus. The global config.json
// fields live under the "Global config" entry (one row per file), so reaching
// Campaigns Dir is: top -> Global -> Global config -> Campaigns Dir. huh aborts
// a form on Ctrl+C (\x03), which backs out one menu level at a time.
const (
	enterGlobal       = "\r"             // top menu: Global is the first row
	openGlobalConfig  = "\x1b[B\r"       // global menu: down to Global config, enter
	openCampaignsDir  = "\x1b[B\x1b[B\r" // config sub-menu: down to Campaigns Dir, enter
	abortForm         = "\x03"           // Ctrl+C aborts the current form
	clearInputAndSave = "\x15\r"         // Ctrl+U clears the field, Enter submits
)

func TestCampSettings_CampaignsDirEditAndClear(t *testing.T) {
	skipUnlessSettingsTTYTests(t)
	tc := GetSharedContainer(t)

	output, err := tc.RunCampInteractiveStepsInDir(
		"/test",
		[]InteractiveStep{
			{WaitFor: "Select configuration scope", Input: enterGlobal},
			{WaitFor: "Files under", Input: openGlobalConfig},
			{WaitFor: "Campaigns Dir", Input: openCampaignsDir},
			{WaitFor: "Where 'camp create' places new campaigns", Input: "/tmp/settings-campaigns\r"},
			{WaitFor: "/tmp/settings-campaigns", Input: abortForm},    // config sub-menu shows new value; back out
			{WaitFor: "Files under", Input: abortForm},                // global menu; back out
			{WaitFor: "Select configuration scope", Input: abortForm}, // top menu; exit
		},
		"--no-color", "settings",
	)
	require.NoError(t, err, "camp settings should save Campaigns Dir; output:\n%s", output)
	assert.Contains(t, output, "Campaigns Dir")

	configJSON, err := tc.ReadFile("/root/.obey/campaign/config.json")
	require.NoError(t, err)
	assert.Contains(t, configJSON, `"campaigns_dir": "/tmp/settings-campaigns"`)

	clearOutput, err := tc.RunCampInteractiveStepsInDir(
		"/test",
		[]InteractiveStep{
			{WaitFor: "Select configuration scope", Input: enterGlobal},
			{WaitFor: "Files under", Input: openGlobalConfig},
			{WaitFor: "/tmp/settings-campaigns", Input: openCampaignsDir},
			{WaitFor: "Where 'camp create' places new campaigns", Input: clearInputAndSave},
			{WaitFor: "~/campaigns", Input: abortForm},
			{WaitFor: "Files under", Input: abortForm},
			{WaitFor: "Select configuration scope", Input: abortForm},
		},
		"--no-color", "settings",
	)
	require.NoError(t, err, "camp settings should clear Campaigns Dir; output:\n%s", clearOutput)

	configJSON, err = tc.ReadFile("/root/.obey/campaign/config.json")
	require.NoError(t, err)
	assert.NotContains(t, configJSON, "campaigns_dir")
}
