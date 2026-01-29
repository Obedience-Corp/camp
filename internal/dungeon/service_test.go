package dungeon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestService_Init(t *testing.T) {
	ctx := context.Background()

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	// Test initial creation
	result, err := svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Should create directories (dungeon, completed, archived, someday)
	if len(result.CreatedDirs) != 4 {
		t.Errorf("expected 4 created dirs, got %d: %v", len(result.CreatedDirs), result.CreatedDirs)
	}

	// Should create files (OBEY.md only)
	if len(result.CreatedFiles) != 1 {
		t.Errorf("expected 1 created file, got %d: %v", len(result.CreatedFiles), result.CreatedFiles)
	}

	// Verify OBEY.md exists
	if _, err := os.Stat(filepath.Join(dungeonPath, "OBEY.md")); os.IsNotExist(err) {
		t.Error("OBEY.md was not created")
	}

	// Verify subdirectories exist
	for _, subdir := range []string{"completed", "archived", "someday"} {
		if _, err := os.Stat(filepath.Join(dungeonPath, subdir)); os.IsNotExist(err) {
			t.Errorf("%s/ was not created", subdir)
		}
	}
}

func TestService_Init_Idempotent(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	// First init
	_, err = svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("first Init failed: %v", err)
	}

	// Second init should not fail and should skip existing files
	result, err := svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("second Init failed: %v", err)
	}

	if len(result.CreatedFiles) != 0 {
		t.Errorf("expected 0 created files on second init, got %d", len(result.CreatedFiles))
	}

	// Only OBEY.md should be skipped
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped file on second init, got %d: %v", len(result.Skipped), result.Skipped)
	}
}

func TestService_Init_Force(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	// First init
	_, err = svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("first Init failed: %v", err)
	}

	// Second init with force
	result, err := svc.Init(ctx, InitOptions{Force: true})
	if err != nil {
		t.Fatalf("second Init with force failed: %v", err)
	}

	// Only OBEY.md should be recreated with force
	if len(result.CreatedFiles) != 1 {
		t.Errorf("expected 1 created file with force, got %d: %v", len(result.CreatedFiles), result.CreatedFiles)
	}
}

func TestService_ListItems(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	// Init dungeon
	_, err = svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Add test items
	testFile := filepath.Join(dungeonPath, "test-file.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	testDir := filepath.Join(dungeonPath, "test-dir")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	// List items
	items, err := svc.ListItems(ctx)
	if err != nil {
		t.Fatalf("ListItems failed: %v", err)
	}

	// Should have 2 items (excluding completed/, archived/, someday/, OBEY.md, crawl.jsonl)
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	// Verify types
	foundFile := false
	foundDir := false
	for _, item := range items {
		if item.Name == "test-file.txt" && item.Type == ItemTypeFile {
			foundFile = true
		}
		if item.Name == "test-dir/" && item.Type == ItemTypeDirectory {
			foundDir = true
		}
	}

	if !foundFile {
		t.Error("test-file.txt not found or wrong type")
	}
	if !foundDir {
		t.Error("test-dir/ not found or wrong type")
	}
}

func TestService_Archive(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	// Init dungeon
	_, err = svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Add test file
	testFile := filepath.Join(dungeonPath, "to-archive.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Archive it
	if err := svc.Archive(ctx, "to-archive.txt"); err != nil {
		t.Fatalf("Archive failed: %v", err)
	}

	// Verify it moved
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("file should not exist in dungeon root after archive")
	}

	archivedFile := filepath.Join(dungeonPath, "archived", "to-archive.txt")
	if _, err := os.Stat(archivedFile); os.IsNotExist(err) {
		t.Error("file should exist in archived/ after archive")
	}
}

func TestService_Archive_NotFound(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	// Init dungeon
	_, err = svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Try to archive non-existent file
	err = svc.Archive(ctx, "nonexistent.txt")
	if err == nil {
		t.Fatal("Archive should fail for non-existent file")
	}
}

func TestService_AppendCrawlLog(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	// Init dungeon
	_, err = svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Append entry
	entry := CrawlEntry{
		Item:     "test-item",
		Decision: DecisionKeep,
	}

	if err := svc.AppendCrawlLog(ctx, entry); err != nil {
		t.Fatalf("AppendCrawlLog failed: %v", err)
	}

	// Verify file exists and has content
	logPath := filepath.Join(dungeonPath, "crawl.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read crawl log: %v", err)
	}

	if len(data) == 0 {
		t.Error("crawl log should not be empty")
	}

	// Append another entry
	entry2 := CrawlEntry{
		Item:     "test-item-2",
		Decision: DecisionArchive,
	}

	if err := svc.AppendCrawlLog(ctx, entry2); err != nil {
		t.Fatalf("second AppendCrawlLog failed: %v", err)
	}

	// Verify two lines
	data, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read crawl log: %v", err)
	}

	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}

	if lines != 2 {
		t.Errorf("expected 2 lines in crawl log, got %d", lines)
	}
}
