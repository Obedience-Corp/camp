//go:build integration
// +build integration

package integration

import (
	"fmt"
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

// TestIntentAdd_ConceptPickerSelectsWorkflowDirectory drives compiled
// `camp intent add` against a workflow concept limited to depth 1 (as older
// campaign configs are) and associates intents with workflow directories
// themselves: an ad-hoc custom workflow selected directly, and a configured
// child selected through the "this directory" option after drilling in.
func TestIntentAdd_ConceptPickerSelectsWorkflowDirectory(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		pick        []InteractiveStep
		wantScreen  []string
		wantConcept string
	}{
		{
			name:  "ad-hoc workflow selects directly",
			title: "blog-dir-proof",
			pick: []InteractiveStep{
				{WaitFor: "festivals", Input: "blog"},
				{WaitFor: "filter: blog", Input: "\r"},
			},
			wantConcept: "workflow/blog",
		},
		{
			name:  "configured child selected via this directory",
			title: "design-dir-proof",
			pick: []InteractiveStep{
				{WaitFor: "festivals", Input: "design"},
				{WaitFor: "filter: design", Input: "\r"},
				{WaitFor: "(this directory)", Input: "this dir"},
				{WaitFor: "filter: this dir", Input: "\r"},
			},
			wantScreen:  []string{"workflow > design", "auth-doc", "Use workflow/design (this directory)"},
			wantConcept: "workflow/design",
		},
	}

	tc := GetSharedContainer(t)
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			campaignPath := fmt.Sprintf("/campaigns/intent-add-workflow-dir-%d", i)
			_, err := tc.InitCampaign(campaignPath, fmt.Sprintf("intent-add-workflow-dir-%d", i), "product")
			require.NoError(t, err)

			cfg := campaignPath + "/.campaign/campaign.yaml"
			tc.Shell(t, `sed -i '/^ *- name: workflow$/{n;s|^\( *\)path: workflow/$|&\n\1depth: 1|}' `+cfg)
			depthCfg := tc.Shell(t, `grep -A2 -- '- name: workflow$' `+cfg)
			require.Contains(t, depthCfg, "depth: 1", "fixture must limit workflow depth")

			tc.Shell(t, "mkdir -p "+campaignPath+"/workflow/design/auth-doc "+campaignPath+"/workflow/blog/posts")

			steps := []InteractiveStep{
				{WaitFor: "enter continue", Input: tt.title + "\r"},
				{WaitFor: "Select type", Input: ""},
				{WaitFor: "type to filter", Input: "\r"},
				{WaitFor: "(none)", Input: "workflow"},
				{WaitFor: "filter: workflow", Input: "\r"},
			}
			steps = append(steps, tt.pick...)
			steps = append(steps,
				InteractiveStep{WaitFor: "Ctrl+S: save", Input: "\x13"},
				InteractiveStep{WaitFor: "Idea created", Input: ""},
			)

			output, err := tc.runCampInteractive(campaignPath, nil, 45*time.Second, steps, "intent", "add", "--no-commit")
			require.NoError(t, err, "intent add TTY session failed; output:\n%s", output)
			assert.NotContains(t, output, "(no items)")
			for _, want := range tt.wantScreen {
				assert.Contains(t, output, want)
			}

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
			assert.Contains(t, body, tt.title)
			assert.Contains(t, body, "concept: "+tt.wantConcept)
		})
	}
}
