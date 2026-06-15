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

	// Should create files (OBEY.md + 3 .gitkeep files + .crawl.yaml)
	if len(result.CreatedFiles) != 5 {
		t.Errorf("expected 5 created files, got %d: %v", len(result.CreatedFiles), result.CreatedFiles)
	}

	// Verify OBEY.md exists
	if _, err := os.Stat(filepath.Join(dungeonPath, "OBEY.md")); os.IsNotExist(err) {
		t.Error("OBEY.md was not created")
	}
	crawlConfigPath := filepath.Join(dungeonPath, ".crawl.yaml")
	crawlConfig, err := os.ReadFile(crawlConfigPath)
	if err != nil {
		t.Fatalf(".crawl.yaml was not created: %v", err)
	}
	wantCrawlConfig := "excludes:\n  - festivals\n  - projects\n"
	if string(crawlConfig) != wantCrawlConfig {
		t.Fatalf(".crawl.yaml = %q, want %q", string(crawlConfig), wantCrawlConfig)
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

	// OBEY.md and .crawl.yaml should be skipped
	if len(result.Skipped) != 2 {
		t.Errorf("expected 2 skipped files on second init, got %d: %v", len(result.Skipped), result.Skipped)
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

	// OBEY.md and .crawl.yaml should be recreated with force
	if len(result.CreatedFiles) != 2 {
		t.Errorf("expected 2 created files with force, got %d: %v", len(result.CreatedFiles), result.CreatedFiles)
	}
}
