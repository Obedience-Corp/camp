package tokens

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lancekrogers/tcount/tokenizer"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func TestCountFile_ExactTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte("Hello, world!"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := NewCounter("gpt-4o", dir)
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	count, err := c.CountFile(context.Background(), path)
	if err != nil {
		t.Fatalf("CountFile: %v", err)
	}
	if count != 4 {
		t.Errorf("count = %d, want 4", count)
	}
}

func TestCountFile_CacheHitOnUnchangedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	content := []byte("The quick brown fox jumps over the lazy dog.")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := NewCounter("gpt-4o", dir)
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	first, err := c.CountFile(context.Background(), path)
	if err != nil {
		t.Fatalf("CountFile first: %v", err)
	}
	if first <= 0 {
		t.Fatalf("first count = %d, want > 0", first)
	}
	// Second call must return the same count (cache hit, not recount).
	second, err := c.CountFile(context.Background(), path)
	if err != nil {
		t.Fatalf("CountFile second: %v", err)
	}
	if second != first {
		t.Errorf("cached count = %d, want %d", second, first)
	}
}

func TestCountFile_CacheInvalidatedOnContentChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := NewCounter("gpt-4o", dir)
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	first, err := c.CountFile(context.Background(), path)
	if err != nil {
		t.Fatalf("CountFile first: %v", err)
	}
	// Change the content.
	if err := os.WriteFile(path, []byte("a much longer document with many more tokens than before"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := c.CountFile(context.Background(), path)
	if err != nil {
		t.Fatalf("CountFile second: %v", err)
	}
	if second == first {
		t.Errorf("count unchanged after content edit: got %d both times", first)
	}
	if second <= first {
		t.Errorf("expected larger count after adding content: first=%d second=%d", first, second)
	}
}

func TestCountFile_CachePersistedAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte("Persisted cache test."), 0o644); err != nil {
		t.Fatal(err)
	}
	c1, err := NewCounter("gpt-4o", dir)
	if err != nil {
		t.Fatalf("NewCounter c1: %v", err)
	}
	count1, err := c1.CountFile(context.Background(), path)
	if err != nil {
		t.Fatalf("CountFile c1: %v", err)
	}
	if err := c1.SaveCache(); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	// New counter instance loading from the persisted cache file.
	c2, err := NewCounter("gpt-4o", dir)
	if err != nil {
		t.Fatalf("NewCounter c2: %v", err)
	}
	count2, err := c2.CountFile(context.Background(), path)
	if err != nil {
		t.Fatalf("CountFile c2: %v", err)
	}
	if count2 != count1 {
		t.Errorf("persisted cache count = %d, want %d", count2, count1)
	}
}

func TestAnnotateItems_SetsTokenCount(t *testing.T) {
	dir := t.TempDir()
	docPath := filepath.Join(dir, "intent.md")
	if err := os.WriteFile(docPath, []byte("Hello, world!"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := NewCounter("gpt-4o", dir)
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	items := []wkitem.WorkItem{
		{Key: "intent:test", RelativePath: "intent.md", PrimaryDoc: "intent.md"},
		{Key: "intent:nopath", RelativePath: "", PrimaryDoc: ""},
	}
	AnnotateItems(context.Background(), c, dir, items)
	if items[0].TokenCount != 4 {
		t.Errorf("items[0].TokenCount = %d, want 4", items[0].TokenCount)
	}
	if items[1].TokenCount != 0 {
		t.Errorf("items[1].TokenCount = %d, want 0 (no path)", items[1].TokenCount)
	}
}

func TestAnnotateItems_RespectsContextCancellation(t *testing.T) {
	dir := t.TempDir()
	docPath := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(docPath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := NewCounter("gpt-4o", dir)
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items := []wkitem.WorkItem{
		{Key: "a", RelativePath: "doc.md", PrimaryDoc: "doc.md"},
	}
	AnnotateItems(ctx, c, dir, items)
	// Cancelled context means no count is set.
	if items[0].TokenCount != 0 {
		t.Errorf("TokenCount = %d, want 0 (cancelled context)", items[0].TokenCount)
	}
}

func TestNewCounter_DefaultModelWhenEmpty(t *testing.T) {
	c, err := NewCounter("", t.TempDir())
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	if c.model != DefaultModel {
		t.Errorf("model = %q, want %q", c.model, DefaultModel)
	}
}

func TestTokensFromMethods_PrefersExactThenFirst(t *testing.T) {
	tests := []struct {
		name    string
		methods []tokenizer.MethodResult
		want    int
	}{
		{name: "empty", want: 0},
		{
			name: "first exact",
			methods: []tokenizer.MethodResult{
				{Name: "exact", Tokens: 4, IsExact: true},
			},
			want: 4,
		},
		{
			name: "exact not first",
			methods: []tokenizer.MethodResult{
				{Name: "approx", Tokens: 10, IsExact: false},
				{Name: "exact", Tokens: 42, IsExact: true},
			},
			want: 42,
		},
		{
			name: "no exact uses first",
			methods: []tokenizer.MethodResult{
				{Name: "character_based_div4", Tokens: 21, IsExact: false},
				{Name: "word_based_mul133", Tokens: 17, IsExact: false},
				{Name: "whitespace_split", Tokens: 13, IsExact: false},
			},
			want: 21,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tokensFromMethods(tc.methods)
			if got != tc.want {
				t.Errorf("tokensFromMethods() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCountFile_UnknownModelUsesSingleMethodNotSum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	content := "The campaign work item token counter must not sum alternative estimators."
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	const unknownModel = "not-a-real-model"
	c, err := NewCounter(unknownModel, dir)
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	count, err := c.CountFile(context.Background(), path)
	if err != nil {
		t.Fatalf("CountFile: %v", err)
	}

	tc, err := tokenizer.NewCounter(tokenizer.CounterOptions{})
	if err != nil {
		t.Fatalf("tokenizer.NewCounter: %v", err)
	}
	result, err := tc.Count(context.Background(), content, unknownModel, false)
	if err != nil {
		t.Fatalf("tcount Count: %v", err)
	}
	if len(result.Methods) < 2 {
		t.Fatalf("unknown model should return multiple methods, got %d", len(result.Methods))
	}
	want := 0
	foundExact := false
	for _, m := range result.Methods {
		if m.IsExact {
			want = m.Tokens
			foundExact = true
			break
		}
	}
	if !foundExact {
		want = result.Methods[0].Tokens
	}
	sum := 0
	for _, m := range result.Methods {
		sum += m.Tokens
	}
	if sum == want {
		t.Fatalf("sum of methods (%d) equals chosen method; test cannot detect summing", sum)
	}
	if count != want {
		t.Errorf("CountFile = %d, want single method %d (sum would be %d across %d methods)", count, want, sum, len(result.Methods))
	}
}

func TestCountFile_NonexistentFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCounter("gpt-4o", dir)
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	_, err = c.CountFile(context.Background(), filepath.Join(dir, "nope.md"))
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}
