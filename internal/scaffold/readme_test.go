package scaffold

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"text/template"
)

func TestReadmeTemplate(t *testing.T) {
	templateFS, err := fs.Sub(CampaignScaffoldFS, "campaign/templates")
	if err != nil {
		t.Fatalf("failed to get template sub-fs: %v", err)
	}

	tmplData, err := fs.ReadFile(templateFS, "README.md.tmpl")
	if err != nil {
		t.Fatalf("failed to read README.md.tmpl: %v", err)
	}

	tmpl, err := template.New("readme").Parse(string(tmplData))
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	tests := []struct {
		name         string
		vars         map[string]any
		wantContains []string
		wantMissing  []string
	}{
		{
			name: "all variables provided",
			vars: map[string]any{
				"campaign_name":        "my-campaign",
				"campaign_description": "An awesome campaign",
				"campaign_mission":     "Build the future",
			},
			wantContains: []string{
				"# my-campaign",
				"An awesome campaign",
				"## Mission",
				"Build the future",
				"## Directory Structure",
				"## Getting Started",
				"camp --help",
			},
		},
		{
			name: "name only - no description or mission",
			vars: map[string]any{
				"campaign_name":        "bare-campaign",
				"campaign_description": "",
				"campaign_mission":     "",
			},
			wantContains: []string{
				"# bare-campaign",
				"A campaign workspace managed by camp CLI.",
				"## Directory Structure",
				"## Getting Started",
			},
			wantMissing: []string{
				"## Mission",
			},
		},
		{
			name: "name and description but no mission",
			vars: map[string]any{
				"campaign_name":        "partial-campaign",
				"campaign_description": "Has a description",
				"campaign_mission":     "",
			},
			wantContains: []string{
				"# partial-campaign",
				"Has a description",
				"## Directory Structure",
				"## Getting Started",
			},
			wantMissing: []string{
				"## Mission",
			},
		},
		{
			name: "name and mission but no description",
			vars: map[string]any{
				"campaign_name":        "mission-campaign",
				"campaign_description": "",
				"campaign_mission":     "On a mission",
			},
			wantContains: []string{
				"# mission-campaign",
				"A campaign workspace managed by camp CLI.",
				"## Mission",
				"On a mission",
				"## Directory Structure",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, tt.vars); err != nil {
				t.Fatalf("template execution failed: %v", err)
			}

			output := buf.String()

			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("output missing expected string %q\nGot:\n%s", want, output)
				}
			}

			for _, notWant := range tt.wantMissing {
				if strings.Contains(output, notWant) {
					t.Errorf("output contains unexpected string %q\nGot:\n%s", notWant, output)
				}
			}
		})
	}
}
