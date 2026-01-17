package index

import (
	"testing"

	"github.com/obediencecorp/camp/internal/nav"
)

func TestNewQuery(t *testing.T) {
	idx := NewIndex("/test")
	q := NewQuery(idx)

	if q == nil {
		t.Fatal("NewQuery returned nil")
	}
	if q.index != idx {
		t.Error("Query index not set correctly")
	}
}

func TestQuery_All(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	all := q.All()
	if len(all) != 5 {
		t.Errorf("All() returned %d targets, want 5", len(all))
	}
}

func TestQuery_All_NilIndex(t *testing.T) {
	q := NewQuery(nil)
	all := q.All()
	if all != nil {
		t.Error("All() should return nil for nil index")
	}
}

func TestQuery_Count(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	count := q.Count()
	if count != 5 {
		t.Errorf("Count() = %d, want 5", count)
	}
}

func TestQuery_Count_NilIndex(t *testing.T) {
	q := NewQuery(nil)
	count := q.Count()
	if count != 0 {
		t.Errorf("Count() should be 0 for nil index, got %d", count)
	}
}

func TestQuery_ByCategory(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	tests := []struct {
		category nav.Category
		want     int
	}{
		{nav.CategoryProjects, 2},
		{nav.CategoryFestivals, 1},
		{nav.CategoryDocs, 1},
		{nav.CategoryCorpus, 1},
		{nav.CategoryAll, 5},
		{nav.Category("nonexistent"), 0},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			result := q.ByCategory(tt.category)
			if len(result) != tt.want {
				t.Errorf("ByCategory(%q) returned %d targets, want %d", tt.category, len(result), tt.want)
			}
		})
	}
}

func TestQuery_ByCategory_NilIndex(t *testing.T) {
	q := NewQuery(nil)
	result := q.ByCategory(nav.CategoryProjects)
	if result != nil {
		t.Error("ByCategory() should return nil for nil index")
	}
}

func TestQuery_Categories(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	cats := q.Categories()
	if len(cats) != 4 {
		t.Errorf("Categories() returned %d categories, want 4", len(cats))
	}

	// Verify all expected categories present
	expected := map[nav.Category]bool{
		nav.CategoryProjects:  true,
		nav.CategoryFestivals: true,
		nav.CategoryDocs:      true,
		nav.CategoryCorpus:    true,
	}

	for _, cat := range cats {
		if !expected[cat] {
			t.Errorf("Unexpected category: %q", cat)
		}
	}
}

func TestQuery_Categories_NilIndex(t *testing.T) {
	q := NewQuery(nil)
	cats := q.Categories()
	if cats != nil {
		t.Error("Categories() should return nil for nil index")
	}
}

func TestQuery_Search(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	tests := []struct {
		name        string
		query       string
		category    nav.Category
		wantAtLeast int // fuzzy search may match more
	}{
		{"empty query all", "", nav.CategoryAll, 5},
		{"empty query projects", "", nav.CategoryProjects, 2},
		{"api search all", "api", nav.CategoryAll, 1}, // at least api-service
		{"api search projects", "api", nav.CategoryProjects, 1},
		{"web search", "web", nav.CategoryAll, 1},
		{"no match", "xyz123", nav.CategoryAll, 0}, // use unique pattern
		{"camp in festivals", "camp", nav.CategoryFestivals, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := q.Search(tt.query, tt.category)
			if len(result) < tt.wantAtLeast {
				t.Errorf("Search(%q, %q) returned %d results, want at least %d", tt.query, tt.category, len(result), tt.wantAtLeast)
			}
		})
	}
}

func TestQuery_Search_PreservesOrder(t *testing.T) {
	idx := NewIndex("/test")
	idx.AddTarget(Target{Name: "api-service", Category: nav.CategoryProjects})
	idx.AddTarget(Target{Name: "api-gateway", Category: nav.CategoryProjects})
	idx.AddTarget(Target{Name: "api", Category: nav.CategoryProjects})

	q := NewQuery(idx)
	results := q.Search("api", nav.CategoryAll)

	if len(results) < 1 {
		t.Fatal("Expected at least one result")
	}

	// Exact match should come first (handled by fuzzy scorer)
	if results[0].Name != "api" {
		t.Errorf("Expected exact match 'api' first, got %q", results[0].Name)
	}
}

func TestQuery_Complete(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	tests := []struct {
		prefix   string
		category nav.Category
		wantLen  int
	}{
		{"api", nav.CategoryAll, 1},
		{"API", nav.CategoryAll, 1}, // case insensitive
		{"web", nav.CategoryProjects, 1},
		{"camp", nav.CategoryFestivals, 1},
		{"zzz", nav.CategoryAll, 0},
		{"", nav.CategoryAll, 5}, // empty prefix matches all
	}

	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			result := q.Complete(tt.prefix, tt.category)
			if len(result) != tt.wantLen {
				t.Errorf("Complete(%q, %q) returned %d results, want %d", tt.prefix, tt.category, len(result), tt.wantLen)
			}
		})
	}
}

func TestQuery_CompleteAny(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	tests := []struct {
		partial  string
		category nav.Category
		wantLen  int
	}{
		{"api", nav.CategoryAll, 1},
		{"service", nav.CategoryProjects, 1}, // contains "service"
		{"app", nav.CategoryProjects, 1},     // web-app contains "app"
		{"cli", nav.CategoryFestivals, 1},    // camp-cli contains "cli"
		{"zzz", nav.CategoryAll, 0},
	}

	for _, tt := range tests {
		t.Run(tt.partial, func(t *testing.T) {
			result := q.CompleteAny(tt.partial, tt.category)
			if len(result) != tt.wantLen {
				t.Errorf("CompleteAny(%q, %q) returned %d results, want %d", tt.partial, tt.category, len(result), tt.wantLen)
			}
		})
	}
}

func TestQuery_Find(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	// Find existing target
	result := q.Find("api-service")
	if result == nil {
		t.Fatal("Find() returned nil for existing target")
	}
	if result.Name != "api-service" {
		t.Errorf("Find() returned wrong target: %q", result.Name)
	}

	// Find non-existent target
	result = q.Find("nonexistent")
	if result != nil {
		t.Error("Find() should return nil for non-existent target")
	}
}

func TestQuery_Find_NilIndex(t *testing.T) {
	q := NewQuery(nil)
	result := q.Find("api-service")
	if result != nil {
		t.Error("Find() should return nil for nil index")
	}
}

func TestQuery_FindInCategory(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	// Find in correct category
	result := q.FindInCategory("api-service", nav.CategoryProjects)
	if result == nil {
		t.Fatal("FindInCategory() returned nil for existing target")
	}

	// Find in wrong category
	result = q.FindInCategory("api-service", nav.CategoryFestivals)
	if result != nil {
		t.Error("FindInCategory() should return nil when target not in category")
	}

	// Find with CategoryAll
	result = q.FindInCategory("api-service", nav.CategoryAll)
	if result == nil {
		t.Error("FindInCategory() with CategoryAll should find target")
	}
}

func TestQuery_FindInCategory_NilIndex(t *testing.T) {
	q := NewQuery(nil)
	result := q.FindInCategory("api-service", nav.CategoryProjects)
	if result != nil {
		t.Error("FindInCategory() should return nil for nil index")
	}
}

func TestQuery_Names(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	names := q.Names()
	if len(names) != 5 {
		t.Errorf("Names() returned %d names, want 5", len(names))
	}

	// Check all names are present
	expected := map[string]bool{
		"api-service":  true,
		"web-app":      true,
		"camp-cli":     true,
		"architecture": true,
		"research":     true,
	}

	for _, name := range names {
		if !expected[name] {
			t.Errorf("Unexpected name: %q", name)
		}
	}
}

func TestQuery_Names_NilIndex(t *testing.T) {
	q := NewQuery(nil)
	names := q.Names()
	if names != nil {
		t.Error("Names() should return nil for nil index")
	}
}

func TestQuery_NamesByCategory(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	names := q.NamesByCategory(nav.CategoryProjects)
	if len(names) != 2 {
		t.Errorf("NamesByCategory(projects) returned %d names, want 2", len(names))
	}
}

func TestQuery_Paths(t *testing.T) {
	idx := createTestIndex()
	q := NewQuery(idx)

	paths := q.Paths()
	if len(paths) != 5 {
		t.Errorf("Paths() returned %d paths, want 5", len(paths))
	}
}

func TestQuery_Paths_NilIndex(t *testing.T) {
	q := NewQuery(nil)
	paths := q.Paths()
	if paths != nil {
		t.Error("Paths() should return nil for nil index")
	}
}

// createTestIndex creates a test index with sample targets.
func createTestIndex() *Index {
	idx := NewIndex("/test")
	idx.AddTarget(Target{Name: "api-service", Path: "/test/projects/api-service", Category: nav.CategoryProjects})
	idx.AddTarget(Target{Name: "web-app", Path: "/test/projects/web-app", Category: nav.CategoryProjects})
	idx.AddTarget(Target{Name: "camp-cli", Path: "/test/festivals/camp-cli", Category: nav.CategoryFestivals})
	idx.AddTarget(Target{Name: "architecture", Path: "/test/docs/architecture", Category: nav.CategoryDocs})
	idx.AddTarget(Target{Name: "research", Path: "/test/corpus/research", Category: nav.CategoryCorpus})
	return idx
}

// Benchmarks

func BenchmarkQuery_All(b *testing.B) {
	idx := createLargeIndex(1000)
	q := NewQuery(idx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.All()
	}
}

func BenchmarkQuery_ByCategory(b *testing.B) {
	idx := createLargeIndex(1000)
	q := NewQuery(idx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.ByCategory(nav.CategoryProjects)
	}
}

func BenchmarkQuery_Search(b *testing.B) {
	idx := createLargeIndex(1000)
	q := NewQuery(idx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.Search("api", nav.CategoryAll)
	}
}

func BenchmarkQuery_Complete(b *testing.B) {
	idx := createLargeIndex(1000)
	q := NewQuery(idx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.Complete("api", nav.CategoryAll)
	}
}

func BenchmarkQuery_Find(b *testing.B) {
	idx := createLargeIndex(1000)
	q := NewQuery(idx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.Find("target-500")
	}
}

func createLargeIndex(n int) *Index {
	idx := NewIndex("/test")
	categories := []nav.Category{nav.CategoryProjects, nav.CategoryFestivals, nav.CategoryDocs, nav.CategoryCorpus}

	for i := 0; i < n; i++ {
		cat := categories[i%len(categories)]
		name := "target-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		idx.AddTarget(Target{
			Name:     name,
			Path:     "/test/" + string(cat) + "/" + name,
			Category: cat,
		})
	}
	return idx
}
