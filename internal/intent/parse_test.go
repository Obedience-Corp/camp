package intent

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseIntent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr error
		checks  func(t *testing.T, intent *Intent)
	}{
		// Valid cases
		{
			name: "valid intent with all fields",
			content: `---
id: test-intent-20260119-153412
title: Test Intent
type: feature
concept: test-project
status: active
created_at: 2026-01-19T15:34:12Z
author: lance
priority: high
horizon: now
tags:
  - test
  - example
blocked_by:
  - blocker-20260118-000000
depends_on:
  - dependency-20260117-000000
promotion_criteria: >
  All tests must pass.
---

# Test Intent

Body content here.
`,
			wantErr: nil,
			checks: func(t *testing.T, intent *Intent) {
				if intent.ID != "test-intent-20260119-153412" {
					t.Errorf("ID = %q, want %q", intent.ID, "test-intent-20260119-153412")
				}
				if intent.Title != "Test Intent" {
					t.Errorf("Title = %q, want %q", intent.Title, "Test Intent")
				}
				if intent.Type != TypeFeature {
					t.Errorf("Type = %q, want %q", intent.Type, TypeFeature)
				}
				if intent.Status != StatusActive {
					t.Errorf("Status = %q, want %q", intent.Status, StatusActive)
				}
				if intent.Concept != "test-project" {
					t.Errorf("Concept = %q, want %q", intent.Concept, "test-project")
				}
				if intent.Author != "lance" {
					t.Errorf("Author = %q, want %q", intent.Author, "lance")
				}
				if intent.Priority != PriorityHigh {
					t.Errorf("Priority = %q, want %q", intent.Priority, PriorityHigh)
				}
				if intent.Horizon != HorizonNow {
					t.Errorf("Horizon = %q, want %q", intent.Horizon, HorizonNow)
				}
				if len(intent.Tags) != 2 {
					t.Errorf("Tags length = %d, want 2", len(intent.Tags))
				}
				if len(intent.BlockedBy) != 1 {
					t.Errorf("BlockedBy length = %d, want 1", len(intent.BlockedBy))
				}
				if len(intent.DependsOn) != 1 {
					t.Errorf("DependsOn length = %d, want 1", len(intent.DependsOn))
				}
				if !strings.Contains(intent.PromotionCriteria, "All tests must pass") {
					t.Errorf("PromotionCriteria should contain 'All tests must pass'")
				}
				if !strings.Contains(intent.Content, "Body content here") {
					t.Error("Content should contain body")
				}
			},
		},
		{
			name: "valid intent with required fields only",
			content: `---
id: minimal-20260119-153412
title: Minimal Intent
status: inbox
created_at: 2026-01-19
---

Body.
`,
			wantErr: nil,
			checks: func(t *testing.T, intent *Intent) {
				if intent.ID != "minimal-20260119-153412" {
					t.Errorf("ID = %q, want %q", intent.ID, "minimal-20260119-153412")
				}
				if intent.Type != "" {
					t.Errorf("Type should be empty, got %q", intent.Type)
				}
				if intent.Concept != "" {
					t.Errorf("Concept should be empty, got %q", intent.Concept)
				}
			},
		},
		{
			name: "valid intent with empty body",
			content: `---
id: no-body-20260119-153412
title: No Body
status: inbox
created_at: 2026-01-19
---
`,
			wantErr: nil,
			checks: func(t *testing.T, intent *Intent) {
				if intent.Content != "" {
					t.Errorf("Content should be empty, got %q", intent.Content)
				}
			},
		},
		{
			name: "legacy project field converts to concept",
			content: `---
id: legacy-20260119-153412
title: Legacy Project Intent
status: inbox
created_at: 2026-01-19
project: camp
---

Body with legacy project.
`,
			wantErr: nil,
			checks: func(t *testing.T, intent *Intent) {
				// Legacy project "camp" should convert to concept "projects/camp"
				if intent.Concept != "projects/camp" {
					t.Errorf("Concept = %q, want %q", intent.Concept, "projects/camp")
				}
			},
		},
		{
			name: "concept field preferred over legacy project",
			content: `---
id: both-20260119-153412
title: Both Fields Intent
status: inbox
created_at: 2026-01-19
concept: festivals/new-fest
project: old-project
---

Body with both fields.
`,
			wantErr: nil,
			checks: func(t *testing.T, intent *Intent) {
				// Concept should be preferred over legacy project
				if intent.Concept != "festivals/new-fest" {
					t.Errorf("Concept = %q, want %q (concept should be preferred over project)", intent.Concept, "festivals/new-fest")
				}
			},
		},
		{
			name: "valid intent with multiline body",
			content: `---
id: multiline-20260119-153412
title: Multiline Body
status: inbox
created_at: 2026-01-19
---

# Header

Paragraph one.

Paragraph two.

- List item 1
- List item 2
`,
			wantErr: nil,
			checks: func(t *testing.T, intent *Intent) {
				if !strings.Contains(intent.Content, "# Header") {
					t.Error("Content should contain header")
				}
				if !strings.Contains(intent.Content, "Paragraph one") {
					t.Error("Content should contain paragraph one")
				}
				if !strings.Contains(intent.Content, "List item 2") {
					t.Error("Content should contain list items")
				}
			},
		},

		// Error cases
		{
			name:    "empty content",
			content: "",
			wantErr: ErrEmptyContent,
		},
		{
			name:    "whitespace only",
			content: "   \n\t\n   ",
			wantErr: ErrEmptyContent,
		},
		{
			name: "missing opening delimiter",
			content: `id: test
title: Test
status: inbox
---
body`,
			wantErr: ErrInvalidFrontmatter,
		},
		{
			name: "missing closing delimiter",
			content: `---
id: test
title: Test
status: inbox
body`,
			wantErr: ErrInvalidFrontmatter,
		},
		{
			name:    "only one delimiter",
			content: `---`,
			wantErr: ErrInvalidFrontmatter,
		},
		{
			name: "malformed YAML - unclosed bracket",
			content: `---
id: test
title: [unclosed
status: inbox
---
body`,
			wantErr: ErrFrontmatterParse,
		},
		{
			name: "malformed YAML - invalid indentation",
			content: `---
id: test
  title: bad indent
status: inbox
---
body`,
			wantErr: ErrFrontmatterParse,
		},
		{
			name: "malformed YAML - duplicate key",
			content: `---
id: test
title: First
title: Duplicate
status: inbox
---
body`,
			wantErr: ErrFrontmatterParse, // YAML v3 errors on duplicate keys
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, err := ParseIntent([]byte(tt.content))

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("ParseIntent() error = nil, wantErr %v", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ParseIntent() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseIntent() unexpected error = %v", err)
			}

			if tt.checks != nil {
				tt.checks(t, intent)
			}
		})
	}
}

func TestParseIntentFromFile(t *testing.T) {
	content := `---
id: test-20260119-153412
title: Test Intent
status: inbox
created_at: 2026-01-19
---

Body.
`
	path := "/intents/inbox/test-20260119-153412.md"

	intent, err := ParseIntentFromFile(path, []byte(content))
	if err != nil {
		t.Fatalf("ParseIntentFromFile() error = %v", err)
	}

	if intent.Path != path {
		t.Errorf("Path = %q, want %q", intent.Path, path)
	}
	if intent.ID != "test-20260119-153412" {
		t.Errorf("ID = %q, want %q", intent.ID, "test-20260119-153412")
	}
}

func TestSerializeIntent(t *testing.T) {
	tests := []struct {
		name   string
		intent *Intent
		checks func(t *testing.T, data []byte)
	}{
		{
			name: "full intent",
			intent: &Intent{
				ID:        "test-20260119-153412",
				Title:     "Test Intent",
				Status:    StatusInbox,
				Type:      TypeFeature,
				Priority:  PriorityHigh,
				CreatedAt: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
				Content:   "# Test\n\nBody content.",
			},
			checks: func(t *testing.T, data []byte) {
				s := string(data)
				if !strings.HasPrefix(s, "---\n") {
					t.Error("should start with ---")
				}
				if !strings.Contains(s, "id: test-20260119-153412") {
					t.Error("should contain id")
				}
				if !strings.Contains(s, "title: Test Intent") {
					t.Error("should contain title")
				}
				if !strings.Contains(s, "status: inbox") {
					t.Error("should contain status")
				}
				if !strings.Contains(s, "type: feature") {
					t.Error("should contain type")
				}
				if !strings.Contains(s, "# Test") {
					t.Error("should contain body")
				}
			},
		},
		{
			name: "intent with empty content",
			intent: &Intent{
				ID:        "empty-20260119-153412",
				Title:     "Empty Body",
				Status:    StatusInbox,
				CreatedAt: time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC),
				Content:   "",
			},
			checks: func(t *testing.T, data []byte) {
				s := string(data)
				if !strings.Contains(s, "id: empty-20260119-153412") {
					t.Error("should contain id")
				}
				// Should not have extra newlines for empty body
				if strings.Contains(s, "---\n\n\n") {
					t.Error("should not have excessive newlines")
				}
			},
		},
		{
			name: "optional fields omitted",
			intent: &Intent{
				ID:        "minimal-20260119-153412",
				Title:     "Minimal",
				Status:    StatusInbox,
				CreatedAt: time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC),
			},
			checks: func(t *testing.T, data []byte) {
				s := string(data)
				if strings.Contains(s, "type:") {
					t.Error("should not contain empty type field")
				}
				if strings.Contains(s, "concept:") {
					t.Error("should not contain empty concept field")
				}
				if strings.Contains(s, "priority:") {
					t.Error("should not contain empty priority field")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := SerializeIntent(tt.intent)
			if err != nil {
				t.Fatalf("SerializeIntent() error = %v", err)
			}
			tt.checks(t, data)
		})
	}
}

func TestSerializeIntent_Roundtrip(t *testing.T) {
	original := &Intent{
		ID:        "roundtrip-20260119-153412",
		Title:     "Roundtrip Test",
		Status:    StatusActive,
		Type:      TypeFeature,
		Priority:  PriorityMedium,
		Horizon:   HorizonNext,
		Tags:      []string{"test", "roundtrip"},
		CreatedAt: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
		Content:   "# Roundtrip Test\n\nBody content.\n",
	}

	// Serialize
	data, err := SerializeIntent(original)
	if err != nil {
		t.Fatalf("SerializeIntent() error = %v", err)
	}

	// Parse back
	parsed, err := ParseIntent(data)
	if err != nil {
		t.Fatalf("ParseIntent() error = %v", err)
	}

	// Compare fields
	if parsed.ID != original.ID {
		t.Errorf("ID = %q, want %q", parsed.ID, original.ID)
	}
	if parsed.Title != original.Title {
		t.Errorf("Title = %q, want %q", parsed.Title, original.Title)
	}
	if parsed.Status != original.Status {
		t.Errorf("Status = %q, want %q", parsed.Status, original.Status)
	}
	if parsed.Type != original.Type {
		t.Errorf("Type = %q, want %q", parsed.Type, original.Type)
	}
	if parsed.Priority != original.Priority {
		t.Errorf("Priority = %q, want %q", parsed.Priority, original.Priority)
	}
	if parsed.Horizon != original.Horizon {
		t.Errorf("Horizon = %q, want %q", parsed.Horizon, original.Horizon)
	}
	if len(parsed.Tags) != len(original.Tags) {
		t.Errorf("Tags length = %d, want %d", len(parsed.Tags), len(original.Tags))
	}
	// Content comparison (trim for normalization)
	if strings.TrimSpace(parsed.Content) != strings.TrimSpace(original.Content) {
		t.Errorf("Content = %q, want %q", parsed.Content, original.Content)
	}
}

func TestParseIntent_ContentPreservation(t *testing.T) {
	// Test that various markdown content is preserved exactly
	contents := []string{
		"# Header\n\nParagraph.",
		"```go\nfunc main() {}\n```",
		"| Col1 | Col2 |\n|------|------|\n| A    | B    |",
		"- List item 1\n- List item 2\n  - Nested",
		"> Blockquote\n> continued",
		"Text with `inline code` and **bold**.",
	}

	for _, body := range contents {
		content := "---\nid: test\ntitle: Test\nstatus: inbox\ncreated_at: 2026-01-19\n---\n\n" + body

		intent, err := ParseIntent([]byte(content))
		if err != nil {
			t.Fatalf("ParseIntent() error for body %q: %v", body, err)
		}

		if !strings.Contains(intent.Content, strings.TrimSpace(body)) {
			t.Errorf("Content not preserved for body %q, got %q", body, intent.Content)
		}
	}
}

func TestParseIntent_DateFormats(t *testing.T) {
	// Test various date formats that YAML should handle
	dateFormats := []string{
		"2026-01-19",
		"2026-01-19T15:34:12Z",
		"2026-01-19T15:34:12+00:00",
		"2026-01-19 15:34:12",
	}

	for _, dateStr := range dateFormats {
		content := "---\nid: test\ntitle: Test\nstatus: inbox\ncreated_at: " + dateStr + "\n---\n"

		intent, err := ParseIntent([]byte(content))
		if err != nil {
			t.Errorf("ParseIntent() error for date %q: %v", dateStr, err)
			continue
		}

		if intent.CreatedAt.IsZero() {
			t.Errorf("CreatedAt is zero for date %q", dateStr)
		}
	}
}
