//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_SettingsConceptsEditorRoundTrip drives `camp settings` through
// a real TTY to the campaign-manifest concepts editor, hands off to a scripted
// $EDITOR that rewrites the concepts temp file, and verifies the edit is
// persisted to .campaign/campaign.yaml while the rest of the manifest is left
// intact. Runs entirely inside the shared container so it never touches the host
// filesystem.
func TestIntegration_SettingsConceptsEditorRoundTrip(t *testing.T) {
	tc := GetSharedContainer(t)

	const (
		campaignDir = "/test/settings-concepts"
		campaignYML = campaignDir + "/.campaign/campaign.yaml"
		editorPath  = "/test/concepts-editor.sh"
	)

	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Concepts Round Trip",
		"--type", "product",
		"-d", "Settings concepts integration test",
		"-m", "Verify campaign.yaml concepts round-trip",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	// The scripted editor overwrites the concepts temp file with a known-valid
	// concept list, so the outcome is deterministic regardless of the seeded
	// concepts.
	editorScript := `#!/bin/sh
set -eu
printf 'EDITOR_START\n'
cat > "$1" <<'YAML'
- name: integration-concept
  path: some/integration/path/
  description: added by the integration editor
YAML
printf 'EDITOR_DONE\n'
`
	require.NoError(t, tc.WriteFile(editorPath, editorScript))
	tc.Shell(t, fmt.Sprintf("chmod +x %s", editorPath))

	// huh aborts a form on Ctrl+C (\x03); each abort backs out one menu level,
	// so three unwind manifest -> local -> top and exit camp settings cleanly.
	steps := []InteractiveStep{
		{WaitFor: "Select configuration scope", Input: "\x1b[B\r"}, // top menu: down to Local, enter
		{WaitFor: "Files under .campaign/", Input: "\r"},           // local menu: Campaign manifest (first row), enter
		{WaitFor: "Concepts taxonomy", Input: "\x1b[B\x1b[B\r"},    // manifest menu: down to Concepts, enter
		{WaitFor: "EDITOR_DONE"},                                   // scripted editor rewrote the temp file
		{WaitFor: "Concepts taxonomy", Input: "\x03"},              // back at manifest menu, abort
		{WaitFor: "Files under .campaign/", Input: "\x03"},         // back at local menu, abort
		{WaitFor: "Select configuration scope", Input: "\x03"},     // back at top menu, abort -> exit
	}
	output, err := tc.RunCampInteractiveStepsInDirWithEnv(
		campaignDir,
		map[string]string{"EDITOR": editorPath},
		steps,
		"--no-color", "settings",
	)
	require.NoError(t, err, "interactive settings flow should succeed; output:\n%s", output)

	manifest, err := tc.ReadFile(campaignYML)
	require.NoError(t, err, "campaign.yaml should be readable after edit")

	assert.Contains(t, manifest, "integration-concept", "edited concept name should persist to campaign.yaml")
	assert.Contains(t, manifest, "some/integration/path/", "edited concept path should persist to campaign.yaml")
	assert.Contains(t, manifest, "name: Concepts Round Trip", "campaign name should be preserved through the concepts edit")
}
