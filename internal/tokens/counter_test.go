package tokens

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
