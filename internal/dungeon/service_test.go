package dungeon

import (
	"context"
	"errors"
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

	// Should create files (OBEY.md + 3 .gitkeep files)
	if len(result.CreatedFiles) != 4 {
		t.Errorf("expected 4 created files, got %d: %v", len(result.CreatedFiles), result.CreatedFiles)
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

	// Add test file
	testFile := filepath.Join(dungeonPath, "test-file.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Add test directory at dungeon root (should NOT appear - dirs are excluded as status dirs)
	testDir := filepath.Join(dungeonPath, "test-dir")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	// List items
	items, err := svc.ListItems(ctx)
	if err != nil {
		t.Fatalf("ListItems failed: %v", err)
	}

	// Should have 1 item (only files; directories at root are treated as status dirs)
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}

	if len(items) > 0 && items[0].Name != "test-file.txt" {
		t.Errorf("expected test-file.txt, got %s", items[0].Name)
	}
}

func TestService_ListStatusDirs(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	// Init dungeon (creates completed, archived, someday)
	_, err = svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Add a custom status dir
	readyDir := filepath.Join(dungeonPath, "ready")
	if err := os.MkdirAll(readyDir, 0755); err != nil {
		t.Fatalf("failed to create ready dir: %v", err)
	}

	// Add items to some dirs
	if err := os.WriteFile(filepath.Join(dungeonPath, "completed", "item1.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create item: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dungeonPath, "completed", "item2.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create item: %v", err)
	}

	dirs, err := svc.ListStatusDirs(ctx)
	if err != nil {
		t.Fatalf("ListStatusDirs failed: %v", err)
	}

	// Should have 4 dirs: archived, completed, ready, someday (sorted alphabetically)
	if len(dirs) != 4 {
		t.Fatalf("expected 4 status dirs, got %d", len(dirs))
	}

	// Check alphabetical order
	expected := []string{"archived", "completed", "ready", "someday"}
	for i, d := range dirs {
		if d.Name != expected[i] {
			t.Errorf("dir[%d] = %s, want %s", i, d.Name, expected[i])
		}
	}

	// Check completed has 2 items (+ .gitkeep = 3 entries, but .gitkeep excluded = 2)
	for _, d := range dirs {
		if d.Name == "completed" && d.ItemCount != 2 {
			t.Errorf("completed should have 2 items, got %d", d.ItemCount)
		}
		if d.Name == "ready" && d.ItemCount != 0 {
			t.Errorf("ready should have 0 items, got %d", d.ItemCount)
		}
	}
}

func TestService_ListStatusDirs_Empty(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create dungeon with no subdirs
	dungeonPath := filepath.Join(tmpDir, "dungeon")
	if err := os.MkdirAll(dungeonPath, 0755); err != nil {
		t.Fatalf("failed to create dungeon dir: %v", err)
	}

	svc := NewService(tmpDir, dungeonPath)

	dirs, err := svc.ListStatusDirs(ctx)
	if err != nil {
		t.Fatalf("ListStatusDirs failed: %v", err)
	}

	if len(dirs) != 0 {
		t.Errorf("expected 0 status dirs, got %d", len(dirs))
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

	// Append another entry with MoveDecision
	entry2 := CrawlEntry{
		Item:     "test-item-2",
		Decision: MoveDecision("archived"),
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

func TestService_MoveToStatus(t *testing.T) {
	ctx := context.Background()

	for _, status := range []string{"completed", "archived", "someday"} {
		t.Run(status, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			dungeonPath := filepath.Join(tmpDir, "dungeon")
			svc := NewService(tmpDir, dungeonPath)

			if _, err := svc.Init(ctx, InitOptions{}); err != nil {
				t.Fatalf("Init failed: %v", err)
			}

			// Create test file in dungeon root
			testFile := filepath.Join(dungeonPath, "test-item.txt")
			if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}

			// Move to status
			if err := svc.MoveToStatus(ctx, "test-item.txt", status); err != nil {
				t.Fatalf("MoveToStatus(%s) failed: %v", status, err)
			}

			// Verify removed from root
			if _, err := os.Stat(testFile); !os.IsNotExist(err) {
				t.Error("file should not exist in dungeon root after move")
			}

			// Verify exists in status dir
			movedFile := filepath.Join(dungeonPath, status, "test-item.txt")
			if _, err := os.Stat(movedFile); os.IsNotExist(err) {
				t.Errorf("file should exist in %s/ after move", status)
			}
		})
	}
}

func TestService_MoveToStatus_CustomDir(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create test file in dungeon root
	testFile := filepath.Join(dungeonPath, "test-item.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Move to a custom status "ready" — should work (auto-creates dir)
	if err := svc.MoveToStatus(ctx, "test-item.txt", "ready"); err != nil {
		t.Fatalf("MoveToStatus(ready) failed: %v", err)
	}

	movedFile := filepath.Join(dungeonPath, "ready", "test-item.txt")
	if _, err := os.Stat(movedFile); os.IsNotExist(err) {
		t.Error("file should exist in ready/ after move")
	}
}

func TestService_MoveToStatus_InvalidStatus(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	testFile := filepath.Join(dungeonPath, "test-item.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Path traversal should be rejected
	err = svc.MoveToStatus(ctx, "test-item.txt", "../escape")
	if err == nil {
		t.Fatal("MoveToStatus should fail for path traversal")
	}
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got: %v", err)
	}

	// Empty string should be rejected
	err = svc.MoveToStatus(ctx, "test-item.txt", "")
	if err == nil {
		t.Fatal("MoveToStatus should fail for empty status")
	}
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got: %v", err)
	}

	// Dot-dot should be rejected
	err = svc.MoveToStatus(ctx, "test-item.txt", "..")
	if err == nil {
		t.Fatal("MoveToStatus should fail for '..'")
	}
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got: %v", err)
	}
}

func TestService_MoveToStatus_NotFound(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	err = svc.MoveToStatus(ctx, "nonexistent.txt", "completed")
	if err == nil {
		t.Fatal("MoveToStatus should fail for non-existent item")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestService_MoveToStatus_Collision(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create file in dungeon root
	testFile := filepath.Join(dungeonPath, "collide.txt")
	if err := os.WriteFile(testFile, []byte("root"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create same-named file already in completed/
	existingFile := filepath.Join(dungeonPath, "completed", "collide.txt")
	if err := os.WriteFile(existingFile, []byte("existing"), 0644); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	err = svc.MoveToStatus(ctx, "collide.txt", "completed")
	if err == nil {
		t.Fatal("MoveToStatus should fail on collision")
	}
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got: %v", err)
	}
}

func TestService_MoveToDungeonStatus(t *testing.T) {
	ctx := context.Background()

	for _, status := range []string{"completed", "archived", "someday"} {
		t.Run(status, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			dungeonPath := filepath.Join(tmpDir, "dungeon")
			svc := NewService(tmpDir, dungeonPath)

			if _, err := svc.Init(ctx, InitOptions{}); err != nil {
				t.Fatalf("Init failed: %v", err)
			}

			// Create test file in parent dir (tmpDir)
			testFile := filepath.Join(tmpDir, "parent-item.txt")
			if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}

			// Move directly to status
			if err := svc.MoveToDungeonStatus(ctx, "parent-item.txt", tmpDir, status); err != nil {
				t.Fatalf("MoveToDungeonStatus(%s) failed: %v", status, err)
			}

			// Verify removed from parent
			if _, err := os.Stat(testFile); !os.IsNotExist(err) {
				t.Error("file should not exist in parent after move")
			}

			// Verify exists in status dir
			movedFile := filepath.Join(dungeonPath, status, "parent-item.txt")
			if _, err := os.Stat(movedFile); os.IsNotExist(err) {
				t.Errorf("file should exist in dungeon/%s/ after move", status)
			}
		})
	}
}

func TestService_MoveToDungeonStatus_Collision(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create file in parent dir
	testFile := filepath.Join(tmpDir, "collide.txt")
	if err := os.WriteFile(testFile, []byte("parent"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create same-named file already in archived/
	existingFile := filepath.Join(dungeonPath, "archived", "collide.txt")
	if err := os.WriteFile(existingFile, []byte("existing"), 0644); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	err = svc.MoveToDungeonStatus(ctx, "collide.txt", tmpDir, "archived")
	if err == nil {
		t.Fatal("MoveToDungeonStatus should fail on collision")
	}
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got: %v", err)
	}
}

func TestService_MoveToDungeonStatus_InvalidStatus(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	testFile := filepath.Join(tmpDir, "item.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Path traversal should be rejected
	err = svc.MoveToDungeonStatus(ctx, "item.txt", tmpDir, "../escape")
	if err == nil {
		t.Fatal("MoveToDungeonStatus should fail for path traversal")
	}
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got: %v", err)
	}

	// Empty status should be rejected
	err = svc.MoveToDungeonStatus(ctx, "item.txt", tmpDir, "")
	if err == nil {
		t.Fatal("MoveToDungeonStatus should fail for empty status")
	}
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got: %v", err)
	}
}

func TestValidateStatusName(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{"valid simple", "completed", false},
		{"valid custom", "ready", false},
		{"empty", "", true},
		{"dot-dot", "..", true},
		{"dot", ".", true},
		{"path separator", "foo/bar", true},
		{"parent traversal", "../escape", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStatusName(tt.status)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateStatusName(%q) error = %v, wantErr %v", tt.status, err, tt.wantErr)
			}
		})
	}
}
