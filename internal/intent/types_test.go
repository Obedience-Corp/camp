package intent

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestIntent_YAMLMarshaling(t *testing.T) {
	createdAt := time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC)
	intent := &Intent{
		ID:        "20260119-153412-test-intent",
		Title:     "Test Intent",
		Status:    StatusInbox,
		Type:      TypeFeature,
		Priority:  PriorityMedium,
		Horizon:   HorizonLater,
		CreatedAt: createdAt,
		Tags:      []string{"test", "example"},
		BlockedBy: []string{"20260118-000000-blocker"},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(intent)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	yamlStr := string(data)

	// Verify YAML contains expected fields
	tests := []struct {
		name     string
		contains string
	}{
		{"id field", "id: 20260119-153412-test-intent"},
		{"title field", "title: Test Intent"},
		{"status field", "status: inbox"},
		{"type field", "type: feature"},
		{"priority field", "priority: medium"},
		{"horizon field", "horizon: later"},
		{"tags field", "tags:"},
		{"blocked_by field", "blocked_by:"},
	}

	for _, tt := range tests {
		if !strings.Contains(yamlStr, tt.contains) {
			t.Errorf("YAML missing %s: expected to contain %q", tt.name, tt.contains)
		}
	}

	// Unmarshal back
	var unmarshaled Intent
	if err := yaml.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	// Verify fields match
	if unmarshaled.ID != intent.ID {
		t.Errorf("ID = %v, want %v", unmarshaled.ID, intent.ID)
	}
	if unmarshaled.Title != intent.Title {
		t.Errorf("Title = %v, want %v", unmarshaled.Title, intent.Title)
	}
	if unmarshaled.Status != intent.Status {
		t.Errorf("Status = %v, want %v", unmarshaled.Status, intent.Status)
	}
	if unmarshaled.Type != intent.Type {
		t.Errorf("Type = %v, want %v", unmarshaled.Type, intent.Type)
	}
	if unmarshaled.Priority != intent.Priority {
		t.Errorf("Priority = %v, want %v", unmarshaled.Priority, intent.Priority)
	}
	if unmarshaled.Horizon != intent.Horizon {
		t.Errorf("Horizon = %v, want %v", unmarshaled.Horizon, intent.Horizon)
	}
	if len(unmarshaled.Tags) != len(intent.Tags) {
		t.Errorf("Tags length = %v, want %v", len(unmarshaled.Tags), len(intent.Tags))
	}
	if len(unmarshaled.BlockedBy) != len(intent.BlockedBy) {
		t.Errorf("BlockedBy length = %v, want %v", len(unmarshaled.BlockedBy), len(intent.BlockedBy))
	}
}

func TestIntent_OptionalFieldsOmitted(t *testing.T) {
	intent := &Intent{
		ID:        "20260119-153412-test",
		Title:     "Test",
		Status:    StatusInbox,
		CreatedAt: time.Now(),
	}

	data, err := yaml.Marshal(intent)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	yamlStr := string(data)

	// Should NOT contain optional fields when empty
	optionalFields := []string{
		"type:",
		"project:",
		"author:",
		"priority:",
		"horizon:",
		"tags:",
		"blocked_by:",
		"depends_on:",
		"promotion_criteria:",
		"promoted_to:",
		"updated_at:",
	}

	for _, field := range optionalFields {
		if strings.Contains(yamlStr, field) {
			t.Errorf("YAML should not contain empty %s", field)
		}
	}

	// Should contain required fields
	requiredFields := []string{"id:", "title:", "status:", "created_at:"}
	for _, field := range requiredFields {
		if !strings.Contains(yamlStr, field) {
			t.Errorf("YAML should contain required %s", field)
		}
	}
}

func TestIntent_RuntimeFieldsNotSerialized(t *testing.T) {
	intent := &Intent{
		ID:        "20260119-153412-test",
		Title:     "Test",
		Status:    StatusInbox,
		CreatedAt: time.Now(),
		Path:      "/path/to/intent.md",
		Content:   "# Test\n\nBody content",
	}

	data, err := yaml.Marshal(intent)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	yamlStr := string(data)

	// Runtime fields should NOT appear in YAML
	if strings.Contains(yamlStr, "path:") {
		t.Error("YAML should not contain path field")
	}
	if strings.Contains(yamlStr, "content:") {
		t.Error("YAML should not contain content field")
	}
	if strings.Contains(yamlStr, "/path/to/intent.md") {
		t.Error("YAML should not contain path value")
	}
	if strings.Contains(yamlStr, "Body content") {
		t.Error("YAML should not contain content value")
	}
}

func TestStatus_String(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusInbox, "inbox"},
		{StatusActive, "active"},
		{StatusReady, "ready"},
		{StatusDone, "done"},
		{StatusKilled, "killed"},
	}

	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("Status.String() = %v, want %v", got, tt.want)
		}
	}
}

func TestType_String(t *testing.T) {
	tests := []struct {
		typ  Type
		want string
	}{
		{TypeIdea, "idea"},
		{TypeFeature, "feature"},
		{TypeBug, "bug"},
		{TypeResearch, "research"},
		{TypeChore, "chore"},
	}

	for _, tt := range tests {
		if got := tt.typ.String(); got != tt.want {
			t.Errorf("Type.String() = %v, want %v", got, tt.want)
		}
	}
}

func TestPriority_String(t *testing.T) {
	tests := []struct {
		priority Priority
		want     string
	}{
		{PriorityLow, "low"},
		{PriorityMedium, "medium"},
		{PriorityHigh, "high"},
	}

	for _, tt := range tests {
		if got := tt.priority.String(); got != tt.want {
			t.Errorf("Priority.String() = %v, want %v", got, tt.want)
		}
	}
}

func TestHorizon_String(t *testing.T) {
	tests := []struct {
		horizon Horizon
		want    string
	}{
		{HorizonNow, "now"},
		{HorizonNext, "next"},
		{HorizonLater, "later"},
	}

	for _, tt := range tests {
		if got := tt.horizon.String(); got != tt.want {
			t.Errorf("Horizon.String() = %v, want %v", got, tt.want)
		}
	}
}

func TestIntent_UnmarshalFromFrontmatter(t *testing.T) {
	// Test unmarshaling from a typical frontmatter string
	frontmatter := `
id: 20260119-153412-add-dark-mode
title: Add dark mode toggle
type: feature
project: guild-chat
status: inbox
created_at: 2026-01-19T15:34:12Z
author: lance

priority: medium
horizon: later

blocked_by:
  - 20260118-000000-theme-system
depends_on:
  - 20260117-000000-settings-page

promotion_criteria: >
  Theme system must be implemented first.
  Settings page must be complete.
`

	var intent Intent
	if err := yaml.Unmarshal([]byte(frontmatter), &intent); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	// Verify all fields parsed correctly
	if intent.ID != "20260119-153412-add-dark-mode" {
		t.Errorf("ID = %q, want %q", intent.ID, "20260119-153412-add-dark-mode")
	}
	if intent.Title != "Add dark mode toggle" {
		t.Errorf("Title = %q, want %q", intent.Title, "Add dark mode toggle")
	}
	if intent.Type != TypeFeature {
		t.Errorf("Type = %q, want %q", intent.Type, TypeFeature)
	}
	if intent.Project != "guild-chat" {
		t.Errorf("Project = %q, want %q", intent.Project, "guild-chat")
	}
	if intent.Status != StatusInbox {
		t.Errorf("Status = %q, want %q", intent.Status, StatusInbox)
	}
	if intent.Author != "lance" {
		t.Errorf("Author = %q, want %q", intent.Author, "lance")
	}
	if intent.Priority != PriorityMedium {
		t.Errorf("Priority = %q, want %q", intent.Priority, PriorityMedium)
	}
	if intent.Horizon != HorizonLater {
		t.Errorf("Horizon = %q, want %q", intent.Horizon, HorizonLater)
	}
	if len(intent.BlockedBy) != 1 {
		t.Errorf("BlockedBy length = %d, want 1", len(intent.BlockedBy))
	}
	if len(intent.DependsOn) != 1 {
		t.Errorf("DependsOn length = %d, want 1", len(intent.DependsOn))
	}
	if intent.PromotionCriteria == "" {
		t.Error("PromotionCriteria should not be empty")
	}
}
