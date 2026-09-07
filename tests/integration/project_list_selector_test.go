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

// TestProjectList_SelectorTTY drives compiled `camp project list` through the
// filter overlay and --path-output write inside the container harness.
func TestProjectList_SelectorTTY(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignPath = "/campaigns/project-list-selector"
	_, err := tc.InitCampaign(campaignPath, "project-list-selector", "product")
	require.NoError(t, err)

	seed := map[string][2]string{
		"atlas":    {"go.mod", "module atlas\n"},
		"grok-cli": {"go.mod", "module grok-cli\n"},
		"web":      {"package.json", "{}\n"},
		"notes":    {"README.md", "notes\n"},
	}
	for name, marker := range seed {
		path := campaignPath + "/projects/" + name
		require.NoError(t, tc.CreateGitRepo(path))
		require.NoError(t, tc.WriteFile(path+"/"+marker[0], marker[1]))
	}

	outPath := "/tmp/project-list-selected"
	output, err := tc.runCampInteractive(
		campaignPath,
		nil,
		30*time.Second,
		[]InteractiveStep{
			{WaitFor: "atlas", Input: ""},
			{WaitFor: "grok-cli", Input: "/"},
			{WaitFor: "enter: go", Input: "go"},
			{WaitFor: "atlas", Input: "\r"},
		},
		"project", "list", "--path-output", outPath,
	)
	require.NoError(t, err, "project list TTY session failed; output:\n%s", output)

	assert.Contains(t, output, "atlas")
	assert.Contains(t, output, "grok-cli")
	assert.Contains(t, output, "enter: go")

	chosen, err := tc.ReadFile(outPath)
	require.NoError(t, err)
	chosen = strings.TrimSpace(chosen)
	assert.True(t,
		strings.HasSuffix(chosen, "projects/atlas") || strings.HasSuffix(chosen, "projects/grok-cli"),
		"path-output should be a Go fixture project, got %q", chosen)
}
