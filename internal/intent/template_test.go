package intent

import (
	"strings"
	"testing"
	"time"
)

func TestRenderTemplate(t *testing.T) {
	tests := []struct {
		name    string
		data    TemplateData
		wantErr bool
		checks  []func(t *testing.T, output string)
	}{
		{
			name: "basic template with all fields",
			data: TemplateData{
				ID:        "test-intent-20260119-153412",
				Title:     "Test Intent",
				Type:      "feature",
				Project:   "camp",
				Author:    "lance",
				CreatedAt: "2026-01-19",
			},
			wantErr: false,
			checks: []func(t *testing.T, output string){
				func(t *testing.T, output string) {
					if !strings.Contains(output, "id: test-intent-20260119-153412") {
						t.Error("missing ID in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "title: Test Intent") {
						t.Error("missing title in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "type: feature") {
						t.Error("missing type in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "project: camp") {
						t.Error("missing project in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "author: lance") {
						t.Error("missing author in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "created_at: 2026-01-19") {
						t.Error("missing created_at in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "status: inbox") {
						t.Error("missing default status in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "priority: medium") {
						t.Error("missing default priority in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "horizon: later") {
						t.Error("missing default horizon in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "# Test Intent") {
						t.Error("missing markdown header in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "## Description") {
						t.Error("missing Description section in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "## Context") {
						t.Error("missing Context section in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "## Notes") {
						t.Error("missing Notes section in output")
					}
				},
			},
		},
		{
			name: "empty optional fields",
			data: TemplateData{
				ID:        "minimal-20260119-153412",
				Title:     "Minimal Intent",
				Type:      "",
				Project:   "",
				Author:    "",
				CreatedAt: "2026-01-19",
			},
			wantErr: false,
			checks: []func(t *testing.T, output string){
				func(t *testing.T, output string) {
					if !strings.Contains(output, "id: minimal-20260119-153412") {
						t.Error("missing ID in output")
					}
				},
				func(t *testing.T, output string) {
					if !strings.Contains(output, "title: Minimal Intent") {
						t.Error("missing title in output")
					}
				},
				func(t *testing.T, output string) {
					// Empty fields should still be present in output with empty values
					if !strings.Contains(output, "type:") {
						t.Error("missing type field in output")
					}
				},
			},
		},
		{
			name: "title with quotes",
			data: TemplateData{
				ID:        "quotes-20260119-153412",
				Title:     `Fix "login" issue`,
				Type:      "bug",
				CreatedAt: "2026-01-19",
			},
			wantErr: false,
			checks: []func(t *testing.T, output string){
				func(t *testing.T, output string) {
					if !strings.Contains(output, `Fix "login" issue`) {
						t.Error("title with quotes not preserved")
					}
				},
			},
		},
		{
			name: "title with apostrophe",
			data: TemplateData{
				ID:        "apostrophe-20260119-153412",
				Title:     "Don't forget this",
				Type:      "idea",
				CreatedAt: "2026-01-19",
			},
			wantErr: false,
			checks: []func(t *testing.T, output string){
				func(t *testing.T, output string) {
					if !strings.Contains(output, "Don't forget this") {
						t.Error("title with apostrophe not preserved")
					}
				},
			},
		},
		{
			name: "title with colon",
			data: TemplateData{
				ID:        "colon-20260119-153412",
				Title:     "Fix: Login timeout",
				Type:      "bug",
				CreatedAt: "2026-01-19",
			},
			wantErr: false,
			checks: []func(t *testing.T, output string){
				func(t *testing.T, output string) {
					if !strings.Contains(output, "Fix: Login timeout") {
						t.Error("title with colon not preserved")
					}
				},
			},
		},
		{
			name: "unicode title",
			data: TemplateData{
				ID:        "unicode-20260119-153412",
				Title:     "研究 OAuth2 providers",
				Type:      "research",
				CreatedAt: "2026-01-19",
			},
			wantErr: false,
			checks: []func(t *testing.T, output string){
				func(t *testing.T, output string) {
					if !strings.Contains(output, "研究 OAuth2 providers") {
						t.Error("unicode title not preserved")
					}
				},
			},
		},
		{
			name: "emoji in title",
			data: TemplateData{
				ID:        "emoji-20260119-153412",
				Title:     "Add 🎉 celebration feature",
				Type:      "feature",
				CreatedAt: "2026-01-19",
			},
			wantErr: false,
			checks: []func(t *testing.T, output string){
				func(t *testing.T, output string) {
					if !strings.Contains(output, "🎉") {
						t.Error("emoji in title not preserved")
					}
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := RenderTemplate(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("RenderTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			for _, check := range tt.checks {
				check(t, output)
			}
		})
	}
}

func TestRenderTemplate_ValidFrontmatter(t *testing.T) {
	data := TemplateData{
		ID:        "test-20260119-153412",
		Title:     "Test Intent",
		Type:      "feature",
		Project:   "camp",
		Author:    "lance",
		CreatedAt: "2026-01-19",
	}

	output, err := RenderTemplate(data)
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}

	// Verify output starts with frontmatter delimiter
	if !strings.HasPrefix(output, "---\n") {
		t.Error("output should start with ---")
	}

	// Verify output has closing frontmatter delimiter
	if !strings.Contains(output, "\n---\n") {
		t.Error("output should have closing ---")
	}

	// Verify YAML can be parsed (roundtrip test)
	intent, err := ParseIntent([]byte(output))
	if err != nil {
		t.Fatalf("ParseIntent() error on rendered template = %v", err)
	}

	if intent.ID != data.ID {
		t.Errorf("Parsed ID = %q, want %q", intent.ID, data.ID)
	}
	if intent.Title != data.Title {
		t.Errorf("Parsed Title = %q, want %q", intent.Title, data.Title)
	}
}

func TestFormatCreatedAt(t *testing.T) {
	tests := []struct {
		name string
		time time.Time
		want string
	}{
		{
			name: "simple date",
			time: time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC),
			want: "2026-01-19",
		},
		{
			name: "date with time",
			time: time.Date(2026, 6, 15, 12, 30, 45, 0, time.UTC),
			want: "2026-06-15",
		},
		{
			name: "end of year",
			time: time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
			want: "2026-12-31",
		},
		{
			name: "start of year",
			time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			want: "2026-01-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCreatedAt(tt.time)
			if got != tt.want {
				t.Errorf("FormatCreatedAt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewTemplateData(t *testing.T) {
	intent := &Intent{
		ID:        "test-20260119-153412",
		Title:     "Test Intent",
		Type:      TypeFeature,
		Project:   "camp",
		Author:    "lance",
		CreatedAt: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	}

	data := NewTemplateData(intent)

	if data.ID != intent.ID {
		t.Errorf("ID = %q, want %q", data.ID, intent.ID)
	}
	if data.Title != intent.Title {
		t.Errorf("Title = %q, want %q", data.Title, intent.Title)
	}
	if data.Type != string(intent.Type) {
		t.Errorf("Type = %q, want %q", data.Type, string(intent.Type))
	}
	if data.Project != intent.Project {
		t.Errorf("Project = %q, want %q", data.Project, intent.Project)
	}
	if data.Author != intent.Author {
		t.Errorf("Author = %q, want %q", data.Author, intent.Author)
	}
	if data.CreatedAt != "2026-01-19" {
		t.Errorf("CreatedAt = %q, want %q", data.CreatedAt, "2026-01-19")
	}
}

func TestNewTemplateDataFromInput(t *testing.T) {
	ts := time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC)

	data := NewTemplateDataFromInput("Test Intent", "feature", "camp", "lance", "Test body content", ts)

	// ID should be generated from title and timestamp (format: slug-YYYYMMDD-HHMMSS)
	if !strings.HasSuffix(data.ID, "-20260119-153412") {
		t.Errorf("ID should end with timestamp suffix, got %q", data.ID)
	}
	if !strings.HasPrefix(data.ID, "test-intent-") {
		t.Errorf("ID should start with slugified title, got %q", data.ID)
	}
	if data.Title != "Test Intent" {
		t.Errorf("Title = %q, want %q", data.Title, "Test Intent")
	}
	if data.Type != "feature" {
		t.Errorf("Type = %q, want %q", data.Type, "feature")
	}
	if data.Project != "camp" {
		t.Errorf("Project = %q, want %q", data.Project, "camp")
	}
	if data.Author != "lance" {
		t.Errorf("Author = %q, want %q", data.Author, "lance")
	}
	if data.CreatedAt != "2026-01-19" {
		t.Errorf("CreatedAt = %q, want %q", data.CreatedAt, "2026-01-19")
	}
	if data.Body != "Test body content" {
		t.Errorf("Body = %q, want %q", data.Body, "Test body content")
	}
}

func TestRenderTemplate_RoundTrip(t *testing.T) {
	// Render a template and verify it can be parsed back
	ts := time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC)
	data := NewTemplateDataFromInput("Roundtrip Test", "feature", "camp", "lance", "Round trip body content", ts)

	output, err := RenderTemplate(data)
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}

	intent, err := ParseIntent([]byte(output))
	if err != nil {
		t.Fatalf("ParseIntent() error = %v", err)
	}

	if intent.ID != data.ID {
		t.Errorf("ID = %q, want %q", intent.ID, data.ID)
	}
	if intent.Title != data.Title {
		t.Errorf("Title = %q, want %q", intent.Title, data.Title)
	}
	if string(intent.Type) != data.Type {
		t.Errorf("Type = %q, want %q", intent.Type, data.Type)
	}
	if intent.Project != data.Project {
		t.Errorf("Project = %q, want %q", intent.Project, data.Project)
	}
	if intent.Author != data.Author {
		t.Errorf("Author = %q, want %q", intent.Author, data.Author)
	}

	// Default values should be present
	if intent.Status != StatusInbox {
		t.Errorf("Status = %q, want %q", intent.Status, StatusInbox)
	}
	if intent.Priority != PriorityMedium {
		t.Errorf("Priority = %q, want %q", intent.Priority, PriorityMedium)
	}
	if intent.Horizon != HorizonLater {
		t.Errorf("Horizon = %q, want %q", intent.Horizon, HorizonLater)
	}

	// Body content should include template sections
	if !strings.Contains(intent.Content, "## Description") {
		t.Error("Content should contain Description section")
	}
}
