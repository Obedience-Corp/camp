package feedback

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuilder_BuildOrUpdate_CreatesNew(t *testing.T) {
	dir := t.TempDir()
	builder := NewBuilder(dir)

	festival := FestivalInfo{
		ID:   "CC0004",
		Name: "camp-commands-enhancement",
	}

	obs := []Observation{
		{
			ID:          "001",
			Criteria:    "UX Issues",
			Observation: "Button is confusing",
			Timestamp:   "2026-02-02T21:05:49Z",
		},
		{
			ID:          "002",
			Criteria:    "Missing Features",
			Observation: "Should auto-create planning phase",
			Timestamp:   "2026-02-05T08:06:09Z",
			Severity:    "high",
			Suggestion:  "Add automatic PLANNING phase creation",
		},
	}

	path, created, err := builder.BuildOrUpdate(festival, obs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Error("expected created=true for new intent")
	}

	expectedPath := filepath.Join(dir, "inbox", "FEEDBACK_CC0004.md")
	if path != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, path)
	}

	// Verify file contents
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading intent: %v", err)
	}

	str := string(content)

	// Check frontmatter
	if !strings.Contains(str, "id: FEEDBACK_CC0004") {
		t.Error("missing ID in frontmatter")
	}
	if !strings.Contains(str, "type: feedback") {
		t.Error("missing type in frontmatter")
	}
	if !strings.Contains(str, "status: inbox") {
		t.Error("missing status in frontmatter")
	}
	if !strings.Contains(str, "  - auto-gathered") {
		t.Error("missing auto-gathered tag")
	}

	// Check observations
	if !strings.Contains(str, "## Missing Features") {
		t.Error("missing criteria heading")
	}
	if !strings.Contains(str, "## UX Issues") {
		t.Error("missing criteria heading")
	}
	if !strings.Contains(str, "- [ ] **001**") {
		t.Error("missing observation 001 checkbox")
	}
	if !strings.Contains(str, "- [ ] **002** [high]") {
		t.Error("missing observation 002 with severity")
	}
	if !strings.Contains(str, "> **Suggestion**: Add automatic PLANNING phase creation") {
		t.Error("missing suggestion blockquote")
	}
	if !strings.Contains(str, "camp gather feedback") {
		t.Error("missing footer")
	}
}

func TestBuilder_BuildOrUpdate_UpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	builder := NewBuilder(dir)

	festival := FestivalInfo{
		ID:   "CC0004",
		Name: "camp-commands-enhancement",
	}

	// Create initial intent
	obs1 := []Observation{
		{
			ID:          "001",
			Criteria:    "UX Issues",
			Observation: "Button is confusing",
			Timestamp:   "2026-02-02T21:05:49Z",
		},
	}

	_, _, err := builder.BuildOrUpdate(festival, obs1)
	if err != nil {
		t.Fatalf("creating initial: %v", err)
	}

	// Update with new observation
	obs2 := []Observation{
		{
			ID:          "003",
			Criteria:    "UX Issues",
			Observation: "Another UX issue",
			Timestamp:   "2026-02-06T10:00:00Z",
		},
	}

	path, created, err := builder.BuildOrUpdate(festival, obs2)
	if err != nil {
		t.Fatalf("updating: %v", err)
	}
	if created {
		t.Error("expected created=false for update")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading updated intent: %v", err)
	}

	str := string(content)

	// Both observations should be present
	if !strings.Contains(str, "**001**") {
		t.Error("missing original observation 001")
	}
	if !strings.Contains(str, "**003**") {
		t.Error("missing new observation 003")
	}
}

func TestBuilder_PreservesCheckboxState(t *testing.T) {
	dir := t.TempDir()
	builder := NewBuilder(dir)

	festival := FestivalInfo{
		ID:   "CC0004",
		Name: "camp-commands-enhancement",
	}

	// Create initial intent
	obs := []Observation{
		{
			ID:          "001",
			Criteria:    "UX Issues",
			Observation: "Button is confusing",
			Timestamp:   "2026-02-02T21:05:49Z",
		},
	}

	path, _, err := builder.BuildOrUpdate(festival, obs)
	if err != nil {
		t.Fatalf("creating: %v", err)
	}

	// Manually check off the observation (simulate user editing)
	content, _ := os.ReadFile(path)
	checked := strings.Replace(string(content), "- [ ] **001**", "- [x] **001**", 1)
	os.WriteFile(path, []byte(checked), 0644)

	// Update with new observation
	newObs := []Observation{
		{
			ID:          "002",
			Criteria:    "UX Issues",
			Observation: "Another issue",
			Timestamp:   "2026-02-06T10:00:00Z",
		},
	}

	_, _, err = builder.BuildOrUpdate(festival, newObs)
	if err != nil {
		t.Fatalf("updating: %v", err)
	}

	updated, _ := os.ReadFile(path)
	str := string(updated)

	// Original observation should still be checked
	if !strings.Contains(str, "- [x] **001**") {
		t.Error("checkbox state for 001 was not preserved")
	}
	// New observation should be unchecked
	if !strings.Contains(str, "- [ ] **002**") {
		t.Error("new observation 002 should be unchecked")
	}
}

func TestParseCheckedIDs(t *testing.T) {
	content := `## UX Issues

- [x] **001** (2026-02-02): Something
- [ ] **002** (2026-02-03): Something else
- [x] **003** [high] (2026-02-04): Third thing
`

	result := parseCheckedIDs(content)

	if !result["001"] {
		t.Error("001 should be checked")
	}
	if result["002"] {
		t.Error("002 should not be checked")
	}
	if !result["003"] {
		t.Error("003 should be checked")
	}
}

func TestExtractFrontmatter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "valid frontmatter",
			input: "---\nid: test\ntype: feedback\n---\n\n# Title\n",
			want:  "---\nid: test\ntype: feedback\n---",
		},
		{
			name:  "no frontmatter",
			input: "# Title\n",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFrontmatter(tt.input)
			if got != tt.want {
				t.Errorf("extractFrontmatter() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeObservations(t *testing.T) {
	existing := []Observation{
		{ID: "001", Criteria: "UX", Observation: "First"},
		{ID: "002", Criteria: "UX", Observation: "Second"},
	}

	newObs := []Observation{
		{ID: "002", Criteria: "UX", Observation: "Second (duplicate)"},
		{ID: "003", Criteria: "UX", Observation: "Third"},
	}

	merged := mergeObservations(existing, newObs)

	if len(merged) != 3 {
		t.Fatalf("expected 3 merged, got %d", len(merged))
	}

	// Verify order: existing first, then new
	if merged[0].ID != "001" || merged[1].ID != "002" || merged[2].ID != "003" {
		t.Errorf("unexpected order: %s, %s, %s", merged[0].ID, merged[1].ID, merged[2].ID)
	}

	// Duplicate should keep existing version
	if merged[1].Observation != "Second" {
		t.Error("duplicate should preserve existing observation text")
	}
}

func TestFormatObsDate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2026-02-02T21:05:49Z", "2026-02-02"},
		{"2026-02-05", "2026-02-05"},
		{"2026-02-05T08:06:09Z", "2026-02-05"},
		{"short", "short"},
	}

	for _, tt := range tests {
		got := formatObsDate(tt.input)
		if got != tt.want {
			t.Errorf("formatObsDate(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGroupByCriteria(t *testing.T) {
	obs := []Observation{
		{ID: "001", Criteria: "UX"},
		{ID: "002", Criteria: "Features"},
		{ID: "003", Criteria: "UX"},
	}

	grouped := groupByCriteria(obs)
	if len(grouped) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(grouped))
	}
	if len(grouped["UX"]) != 2 {
		t.Errorf("expected 2 UX observations, got %d", len(grouped["UX"]))
	}
	if len(grouped["Features"]) != 1 {
		t.Errorf("expected 1 Features observation, got %d", len(grouped["Features"]))
	}
}

func TestExtractObsText(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"- [ ] **001** (2026-02-02): Button is confusing", "Button is confusing"},
		{"- [x] **002** [high] (2026-02-05): INGEST should auto-create", "INGEST should auto-create"},
		{"no match here", ""},
	}

	for _, tt := range tests {
		got := extractObsText(tt.line)
		if got != tt.want {
			t.Errorf("extractObsText(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestExtractSeverity(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"- [ ] **001** [high] (2026-02-02): text", "high"},
		{"- [ ] **001** [low] (2026-02-02): text", "low"},
		{"- [ ] **001** (2026-02-02): text", ""},
	}

	for _, tt := range tests {
		got := extractSeverity(tt.line)
		if got != tt.want {
			t.Errorf("extractSeverity(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}
