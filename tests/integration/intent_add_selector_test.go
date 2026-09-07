//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntentAdd_SelectorTTY drives compiled `camp intent add` through the
// shared selector in the container harness: Type/Concept screens, type-to-filter,
// and the saved inbox file. Host tempfile fixtures are not allowed for this
// filesystem-mutating flow.
func TestIntentAdd_SelectorTTY(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignPath = "/campaigns/intent-add-selector"
	_, err := tc.InitCampaign(campaignPath, "intent-add-selector", "product")
	require.NoError(t, err)

	tc.Shell(t, "mkdir -p "+campaignPath+"/projects/agent-simulator "+
		campaignPath+"/projects/build-util "+campaignPath+"/projects/camp")

	output, err := tc.runCampInteractive(
		campaignPath,
		nil,
		45*time.Second,
		[]InteractiveStep{
			{WaitFor: "enter continue", Input: "pty-proof\r"},
			{WaitFor: "Select type", Input: ""},
			{WaitFor: "type to filter", Input: "\r"},
			{WaitFor: "(none)", Input: "proj"},
			{WaitFor: "filter: proj", Input: "\r"},
			{WaitFor: "camp", Input: "\r"},
			{WaitFor: "Ctrl+S: save", Input: "\x13"},
			{WaitFor: "Idea created", Input: ""},
		},
		"intent", "add", "--no-commit",
	)
	require.NoError(t, err, "intent add TTY session failed; output:\n%s", output)

	assert.Contains(t, output, "Select type")
	assert.Contains(t, output, "type to filter")
	assert.Contains(t, output, "(none)")
	assert.Contains(t, output, "projects")
	assert.Contains(t, output, "filter: proj")
	assert.Contains(t, output, "camp")

	assert.Contains(t, output, "Idea created")

	files, err := tc.ListDirectory(campaignPath + "/.campaign/intents/inbox")
	require.NoError(t, err)
	var md []string
	for _, f := range files {
		if strings.HasSuffix(f, ".md") {
			md = append(md, f)
		}
	}
	require.NotEmpty(t, md, "expected a saved inbox intent, files=%v", files)

	body, err := tc.ReadFile(md[0])
	require.NoError(t, err)
	assert.Contains(t, body, "pty-proof")
}
