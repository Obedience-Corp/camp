package project

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatProjects_Table(t *testing.T) {
	projects := []Project{
		{Name: "api", Path: "projects/api", Type: "go"},
		{Name: "frontend", Path: "projects/frontend", Type: "typescript"},
		{Name: "unknown", Path: "projects/unknown", Type: ""},
	}

	var buf bytes.Buffer
	err := FormatProjects(&buf, projects, FormatTable)
	if err != nil {
		t.Fatalf("FormatProjects() error = %v", err)
	}

	output := buf.String()

	// Check header
	if !strings.Contains(output, "NAME") {
		t.Error("output missing NAME header")
	}
	if !strings.Contains(output, "PATH") {
		t.Error("output missing PATH header")
	}
	if !strings.Contains(output, "TYPE") {
		t.Error("output missing TYPE header")
	}

	// Check projects
	if !strings.Contains(output, "api") {
		t.Error("output missing api project")
	}
	if !strings.Contains(output, "frontend") {
		t.Error("output missing frontend project")
	}

	// Check unknown type displays as "-"
	if !strings.Contains(output, "-") {
		t.Error("unknown type should display as '-'")
	}
}

func TestFormatProjects_TableEmpty(t *testing.T) {
	var buf bytes.Buffer
	err := FormatProjects(&buf, nil, FormatTable)
	if err != nil {
		t.Fatalf("FormatProjects() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No projects found") {
		t.Error("empty table should show helpful message")
	}
}

func TestFormatProjects_Simple(t *testing.T) {
	projects := []Project{
		{Name: "api", Path: "projects/api", Type: "go"},
		{Name: "frontend", Path: "projects/frontend", Type: "typescript"},
	}

	var buf bytes.Buffer
	err := FormatProjects(&buf, projects, FormatSimple)
	if err != nil {
		t.Fatalf("FormatProjects() error = %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) != 2 {
		t.Errorf("simple format should have 2 lines, got %d", len(lines))
	}

	if lines[0] != "api" {
		t.Errorf("first line = %q, want %q", lines[0], "api")
	}
	if lines[1] != "frontend" {
		t.Errorf("second line = %q, want %q", lines[1], "frontend")
	}
}

func TestFormatProjects_SimpleEmpty(t *testing.T) {
	var buf bytes.Buffer
	err := FormatProjects(&buf, nil, FormatSimple)
	if err != nil {
		t.Fatalf("FormatProjects() error = %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("empty simple format should have no output, got %q", buf.String())
	}
}

func TestFormatProjects_JSON(t *testing.T) {
	projects := []Project{
		{Name: "api", Path: "projects/api", Type: "go", URL: "git@github.com:org/api.git"},
		{Name: "frontend", Path: "projects/frontend", Type: "typescript", URL: ""},
	}

	var buf bytes.Buffer
	err := FormatProjects(&buf, projects, FormatJSON)
	if err != nil {
		t.Fatalf("FormatProjects() error = %v", err)
	}

	// Parse JSON to verify it's valid
	var parsed []Project
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(parsed) != 2 {
		t.Errorf("JSON has %d projects, want 2", len(parsed))
	}

	if parsed[0].Name != "api" {
		t.Errorf("first project name = %q, want %q", parsed[0].Name, "api")
	}
}

func TestFormatProjects_JSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	err := FormatProjects(&buf, nil, FormatJSON)
	if err != nil {
		t.Fatalf("FormatProjects() error = %v", err)
	}

	// Should output empty array, not null
	var parsed []Project
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed == nil {
		t.Error("empty JSON should be [], not null")
	}
}

func TestFormatProjects_DefaultIsTable(t *testing.T) {
	projects := []Project{
		{Name: "test", Path: "projects/test", Type: "go"},
	}

	var buf bytes.Buffer
	err := FormatProjects(&buf, projects, "invalid")
	if err != nil {
		t.Fatalf("FormatProjects() error = %v", err)
	}

	// Should default to table format
	output := buf.String()
	if !strings.Contains(output, "NAME") {
		t.Error("invalid format should default to table")
	}
}
