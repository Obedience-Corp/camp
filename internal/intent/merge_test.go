package intent

import (
	"strings"
	"testing"
	"time"
)

func TestMergeIntents_Basic(t *testing.T) {
	sources := []*Intent{
		{
			ID:        "20260129-src1",
			Title:     "Source One",
			Status:    StatusInbox,
			Type:      TypeFeature,
			Concept:   "projects/camp",
			Priority:  PriorityMedium,
			Tags:      []string{"auth", "login"},
			CreatedAt: time.Date(2026, 1, 29, 10, 0, 0, 0, time.UTC),
			Path:      "/intents/inbox/20260129-src1.md",
			Content:   "# Source One\n\nFirst source content.",
		},
		{
			ID:        "20260128-src2",
			Title:     "Source Two",
			Status:    StatusActive,
			Type:      TypeFeature,
			Concept:   "projects/camp",
			Priority:  PriorityHigh,
			Tags:      []string{"auth", "security"},
			CreatedAt: time.Date(2026, 1, 28, 12, 0, 0, 0, time.UTC),
			Path:      "/intents/active/20260128-src2.md",
			Content:   "# Source Two\n\nSecond source content.",
		},
	}

	opts := MergeOptions{
		Title: "Unified Auth Feature",
	}

	merged, err := MergeIntents(sources, opts)
	if err != nil {
		t.Fatalf("MergeIntents() error = %v", err)
	}

	// Check basic fields
	if merged.Title != "Unified Auth Feature" {
		t.Errorf("Title = %q, want %q", merged.Title, "Unified Auth Feature")
	}
	if merged.Status != StatusInbox {
		t.Errorf("Status = %q, want %q", merged.Status, StatusInbox)
	}

	// Check ID format (should include timestamp and slug)
	if !strings.Contains(merged.ID, "unified-auth-feature") {
		t.Errorf("ID = %q, should contain slug", merged.ID)
	}

	// Check type resolution (both are feature)
	if merged.Type != TypeFeature {
		t.Errorf("Type = %q, want %q", merged.Type, TypeFeature)
	}

	// Check priority resolution (should be highest = high)
	if merged.Priority != PriorityHigh {
		t.Errorf("Priority = %q, want %q", merged.Priority, PriorityHigh)
	}

	// Check concept resolution (both are projects/camp)
	if merged.Concept != "projects/camp" {
		t.Errorf("Concept = %q, want %q", merged.Concept, "projects/camp")
	}

	// Check tags union
	if len(merged.Tags) != 3 {
		t.Errorf("Tags length = %d, want 3 (auth, login, security)", len(merged.Tags))
	}

	// Check GatheredFrom
	if len(merged.GatheredFrom) != 2 {
		t.Fatalf("GatheredFrom length = %d, want 2", len(merged.GatheredFrom))
	}
	if merged.GatheredFrom[0].ID != "20260129-src1" {
		t.Errorf("GatheredFrom[0].ID = %q, want %q", merged.GatheredFrom[0].ID, "20260129-src1")
	}
	if merged.GatheredFrom[0].Filename != "20260129-src1.md" {
		t.Errorf("GatheredFrom[0].Filename = %q, want %q", merged.GatheredFrom[0].Filename, "20260129-src1.md")
	}

	// Check GatheredAt
	if merged.GatheredAt.IsZero() {
		t.Error("GatheredAt should not be zero")
	}

	// Check content contains source info
	if !strings.Contains(merged.Content, "Source One") {
		t.Error("Content should contain source titles")
	}
	if !strings.Contains(merged.Content, "First source content") {
		t.Error("Content should contain source body")
	}
	if !strings.Contains(merged.Content, "Second source content") {
		t.Error("Content should contain source body")
	}
}

func TestMergeIntents_ExplicitOverrides(t *testing.T) {
	sources := []*Intent{
		{
			ID:        "20260129-src1",
			Title:     "Source One",
			Type:      TypeIdea,
			Priority:  PriorityLow,
			CreatedAt: time.Now(),
			Path:      "/intents/inbox/20260129-src1.md",
		},
		{
			ID:        "20260128-src2",
			Title:     "Source Two",
			Type:      TypeBug,
			Priority:  PriorityMedium,
			CreatedAt: time.Now(),
			Path:      "/intents/inbox/20260128-src2.md",
		},
	}

	opts := MergeOptions{
		Title:    "Override Test",
		Type:     TypeFeature,      // Override
		Priority: PriorityHigh,     // Override
		Concept:  "custom/concept", // Override
		Horizon:  HorizonNow,       // Override
	}

	merged, err := MergeIntents(sources, opts)
	if err != nil {
		t.Fatalf("MergeIntents() error = %v", err)
	}

	// Check overrides applied
	if merged.Type != TypeFeature {
		t.Errorf("Type = %q, want %q (override)", merged.Type, TypeFeature)
	}
	if merged.Priority != PriorityHigh {
		t.Errorf("Priority = %q, want %q (override)", merged.Priority, PriorityHigh)
	}
	if merged.Concept != "custom/concept" {
		t.Errorf("Concept = %q, want %q (override)", merged.Concept, "custom/concept")
	}
	if merged.Horizon != HorizonNow {
		t.Errorf("Horizon = %q, want %q (override)", merged.Horizon, HorizonNow)
	}
}

func TestMergeIntents_TypeResolution(t *testing.T) {
	sources := []*Intent{
		{ID: "1", Title: "A", Type: TypeFeature, CreatedAt: time.Now(), Path: "/a.md"},
		{ID: "2", Title: "B", Type: TypeFeature, CreatedAt: time.Now(), Path: "/b.md"},
		{ID: "3", Title: "C", Type: TypeBug, CreatedAt: time.Now(), Path: "/c.md"},
	}

	merged, _ := MergeIntents(sources, MergeOptions{Title: "Test"})

	// Feature appears 2x, bug 1x - should resolve to feature
	if merged.Type != TypeFeature {
		t.Errorf("Type = %q, want %q (most common)", merged.Type, TypeFeature)
	}
}

func TestMergeIntents_PriorityResolution(t *testing.T) {
	sources := []*Intent{
		{ID: "1", Title: "A", Priority: PriorityLow, CreatedAt: time.Now(), Path: "/a.md"},
		{ID: "2", Title: "B", Priority: PriorityHigh, CreatedAt: time.Now(), Path: "/b.md"},
		{ID: "3", Title: "C", Priority: PriorityMedium, CreatedAt: time.Now(), Path: "/c.md"},
	}

	merged, _ := MergeIntents(sources, MergeOptions{Title: "Test"})

	// Should take highest priority
	if merged.Priority != PriorityHigh {
		t.Errorf("Priority = %q, want %q (highest)", merged.Priority, PriorityHigh)
	}
}

func TestMergeIntents_HorizonResolution(t *testing.T) {
	sources := []*Intent{
		{ID: "1", Title: "A", Horizon: HorizonLater, CreatedAt: time.Now(), Path: "/a.md"},
		{ID: "2", Title: "B", Horizon: HorizonNow, CreatedAt: time.Now(), Path: "/b.md"},
		{ID: "3", Title: "C", Horizon: HorizonNext, CreatedAt: time.Now(), Path: "/c.md"},
	}

	merged, _ := MergeIntents(sources, MergeOptions{Title: "Test"})

	// Should take most urgent horizon
	if merged.Horizon != HorizonNow {
		t.Errorf("Horizon = %q, want %q (most urgent)", merged.Horizon, HorizonNow)
	}
}

func TestMergeIntents_TagsMerge(t *testing.T) {
	sources := []*Intent{
		{ID: "1", Title: "A", Tags: []string{"Auth", "LOGIN"}, CreatedAt: time.Now(), Path: "/a.md"},
		{ID: "2", Title: "B", Tags: []string{"auth", "security"}, CreatedAt: time.Now(), Path: "/b.md"},
	}

	merged, _ := MergeIntents(sources, MergeOptions{Title: "Test"})

	// Tags should be deduplicated (case-insensitive) and sorted
	if len(merged.Tags) != 3 {
		t.Errorf("Tags length = %d, want 3", len(merged.Tags))
	}

	// Should be lowercase and sorted
	expected := []string{"auth", "login", "security"}
	for i, tag := range merged.Tags {
		if tag != expected[i] {
			t.Errorf("Tags[%d] = %q, want %q", i, tag, expected[i])
		}
	}
}

func TestMergeIntents_DependenciesMerge(t *testing.T) {
	sources := []*Intent{
		{
			ID:        "src1",
			Title:     "A",
			BlockedBy: []string{"blocker1", "src2"}, // src2 is another source
			DependsOn: []string{"dep1"},
			CreatedAt: time.Now(),
			Path:      "/a.md",
		},
		{
			ID:        "src2",
			Title:     "B",
			BlockedBy: []string{"blocker2"},
			DependsOn: []string{"dep1", "dep2"},
			CreatedAt: time.Now(),
			Path:      "/b.md",
		},
	}

	merged, _ := MergeIntents(sources, MergeOptions{Title: "Test"})

	// BlockedBy should union but exclude source IDs
	if len(merged.BlockedBy) != 2 {
		t.Errorf("BlockedBy length = %d, want 2 (blocker1, blocker2)", len(merged.BlockedBy))
	}
	for _, b := range merged.BlockedBy {
		if b == "src1" || b == "src2" {
			t.Errorf("BlockedBy should not include source ID %q", b)
		}
	}

	// DependsOn should union
	if len(merged.DependsOn) != 2 {
		t.Errorf("DependsOn length = %d, want 2 (dep1, dep2)", len(merged.DependsOn))
	}
}

func TestMergeIntents_Errors(t *testing.T) {
	// Test: not enough sources
	_, err := MergeIntents([]*Intent{
		{ID: "only-one", Title: "A", CreatedAt: time.Now(), Path: "/a.md"},
	}, MergeOptions{Title: "Test"})
	if err == nil {
		t.Error("MergeIntents should error with only 1 source")
	}

	// Test: empty sources
	_, err = MergeIntents([]*Intent{}, MergeOptions{Title: "Test"})
	if err == nil {
		t.Error("MergeIntents should error with empty sources")
	}

	// Test: missing title
	_, err = MergeIntents([]*Intent{
		{ID: "1", Title: "A", CreatedAt: time.Now(), Path: "/a.md"},
		{ID: "2", Title: "B", CreatedAt: time.Now(), Path: "/b.md"},
	}, MergeOptions{})
	if err == nil {
		t.Error("MergeIntents should error without title")
	}
}

func TestMergeIntents_ContentFormat(t *testing.T) {
	sources := []*Intent{
		{
			ID:        "src1",
			Title:     "Feature Request",
			Type:      TypeFeature,
			Priority:  PriorityHigh,
			CreatedAt: time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC),
			Path:      "/intents/inbox/src1.md",
			Content:   "# Feature Request\n\nThis is the first request.\n\n## Details\n\nMore info here.",
		},
		{
			ID:        "src2",
			Title:     "Related Idea",
			Type:      TypeIdea,
			CreatedAt: time.Date(2026, 1, 28, 0, 0, 0, 0, time.UTC),
			Path:      "/intents/inbox/src2.md",
			Content:   "# Related Idea\n\nAnother idea.",
		},
	}

	merged, _ := MergeIntents(sources, MergeOptions{Title: "Combined Feature"})

	// Check content structure
	content := merged.Content

	// Should have sources table
	if !strings.Contains(content, "## Sources") {
		t.Error("Content should have Sources section")
	}
	if !strings.Contains(content, "| Feature Request |") {
		t.Error("Content should have source in table")
	}

	// Should have "From:" sections
	if !strings.Contains(content, "## From: Feature Request") {
		t.Error("Content should have 'From:' section for first source")
	}
	if !strings.Contains(content, "## From: Related Idea") {
		t.Error("Content should have 'From:' section for second source")
	}

	// Should include body content (without the title heading)
	if !strings.Contains(content, "This is the first request") {
		t.Error("Content should include source body")
	}
	if !strings.Contains(content, "More info here") {
		t.Error("Content should include nested content")
	}

	// Should have gathered notes section
	if !strings.Contains(content, "## Gathered Notes") {
		t.Error("Content should have Gathered Notes section")
	}
}

func TestGenerateSlugFromTitle(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{"Simple Title", "simple-title"},
		{"Title With  Multiple   Spaces", "title-with-multiple-spaces"},
		{"Title-With-Hyphens", "title-with-hyphens"},
		{"Title_With_Underscores", "titlewithunderscores"},
		{"Title With 123 Numbers", "title-with-123-numbers"},
		{"Special!@#$%Characters", "specialcharacters"},
		{"A Very Long Title That Should Be Truncated Because It Exceeds The Maximum Length", "a-very-long-title-that-should-be-truncated-because"},
		{"", ""},
		{"---", ""},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := generateSlugFromTitle(tt.title)
			if got != tt.want {
				t.Errorf("generateSlugFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestExtractBodyContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "with title heading",
			content: "# Title\n\nBody content here.",
			want:    "Body content here.",
		},
		{
			name:    "with blank lines before title",
			content: "\n\n# Title\n\nBody content.",
			want:    "Body content.",
		},
		{
			name:    "with nested headings",
			content: "# Title\n\n## Subtitle\n\nBody.",
			want:    "## Subtitle\n\nBody.",
		},
		{
			name:    "no title heading",
			content: "Just body content.",
			want:    "Just body content.",
		},
		{
			name:    "empty content",
			content: "",
			want:    "",
		},
		{
			name:    "only title",
			content: "# Just a Title",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBodyContent(tt.content)
			if got != tt.want {
				t.Errorf("extractBodyContent() = %q, want %q", got, tt.want)
			}
		})
	}
}
