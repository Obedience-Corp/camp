package dungeon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestService_MoveToStatus(t *testing.T) {
	ctx := context.Background()

	for _, status := range []string{"completed", "archived", "someday"} {
		t.Run(status, func(t *testing.T) {
			today := time.Now().Format("2006-01-02")

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
			if _, err := svc.MoveToStatus(ctx, "test-item.txt", status); err != nil {
				t.Fatalf("MoveToStatus(%s) failed: %v", status, err)
			}

			// Verify removed from root
			if _, err := os.Stat(testFile); !os.IsNotExist(err) {
				t.Error("file should not exist in dungeon root after move")
			}

			// Verify exists in status dir
			movedFile := filepath.Join(dungeonPath, status, today, "test-item.txt")
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
	today := time.Now().Format("2006-01-02")
	if _, err := svc.MoveToStatus(ctx, "test-item.txt", "ready"); err != nil {
		t.Fatalf("MoveToStatus(ready) failed: %v", err)
	}

	movedFile := filepath.Join(dungeonPath, "ready", today, "test-item.txt")
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
	_, err = svc.MoveToStatus(ctx, "test-item.txt", "../escape")
	if err == nil {
		t.Fatal("MoveToStatus should fail for path traversal")
	}
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got: %v", err)
	}

	// Empty string should be rejected
	_, err = svc.MoveToStatus(ctx, "test-item.txt", "")
	if err == nil {
		t.Fatal("MoveToStatus should fail for empty status")
	}
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got: %v", err)
	}

	// Dot-dot should be rejected
	_, err = svc.MoveToStatus(ctx, "test-item.txt", "..")
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

	_, err = svc.MoveToStatus(ctx, "nonexistent.txt", "completed")
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
	today := time.Now().Format("2006-01-02")
	existingDir := filepath.Join(dungeonPath, "completed", today)
	if err := os.MkdirAll(existingDir, 0755); err != nil {
		t.Fatalf("failed to create dated completed dir: %v", err)
	}
	existingFile := filepath.Join(existingDir, "collide.txt")
	if err := os.WriteFile(existingFile, []byte("existing"), 0644); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	_, err = svc.MoveToStatus(ctx, "collide.txt", "completed")
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
			today := time.Now().Format("2006-01-02")

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
			if _, err := svc.MoveToDungeonStatus(ctx, "parent-item.txt", tmpDir, status); err != nil {
				t.Fatalf("MoveToDungeonStatus(%s) failed: %v", status, err)
			}

			// Verify removed from parent
			if _, err := os.Stat(testFile); !os.IsNotExist(err) {
				t.Error("file should not exist in parent after move")
			}

			// Verify exists in status dir
			movedFile := filepath.Join(dungeonPath, status, today, "parent-item.txt")
			if _, err := os.Stat(movedFile); os.IsNotExist(err) {
				t.Errorf("file should exist in dungeon/%s/ after move", status)
			}
		})
	}
}

func TestService_MoveToDungeon_Collision(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)
	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	source := filepath.Join(tmpDir, "collide.md")
	existing := filepath.Join(dungeonPath, "collide.md")
	if err := os.WriteFile(source, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	err := svc.MoveToDungeon(ctx, "collide.md", tmpDir)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("MoveToDungeon() error = %v, want ErrAlreadyExists", err)
	}
	assertDungeonFileContent(t, source, "source")
	assertDungeonFileContent(t, existing, "existing")
}

func TestService_MoveToDungeonStatErrorNotFoundConflation(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission-mode stat failure is platform/user dependent")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()
	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)
	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	parentPath := filepath.Join(tmpDir, "locked")
	if err := os.MkdirAll(parentPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parentPath, "locked.md"), []byte("locked"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parentPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parentPath, 0755) })

	err := svc.MoveToDungeon(ctx, "locked.md", parentPath)
	if err == nil {
		t.Fatal("MoveToDungeon() expected stat error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("MoveToDungeon() error = %v, should preserve non-not-exist stat error", err)
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
	today := time.Now().Format("2006-01-02")
	existingDir := filepath.Join(dungeonPath, "archived", today)
	if err := os.MkdirAll(existingDir, 0755); err != nil {
		t.Fatalf("failed to create dated archived dir: %v", err)
	}
	existingFile := filepath.Join(existingDir, "collide.txt")
	if err := os.WriteFile(existingFile, []byte("existing"), 0644); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	_, err = svc.MoveToDungeonStatus(ctx, "collide.txt", tmpDir, "archived")
	if err == nil {
		t.Fatal("MoveToDungeonStatus should fail on collision")
	}
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got: %v", err)
	}
}

func TestService_MoveToDungeonStatus_DoesNotWriteBucketUnderFestivals(t *testing.T) {
	ctx := context.Background()

	root := t.TempDir()
	dungeonPath := filepath.Join(root, "dungeon")
	svc := NewService(root, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "festivals", "dungeon"), 0755); err != nil {
		t.Fatalf("failed to create festivals dungeon: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "stale-doc.md"), []byte("stale"), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	targetPath, err := svc.MoveToDungeonStatus(ctx, "stale-doc.md", root, "archived")
	if err != nil {
		t.Fatalf("MoveToDungeonStatus failed: %v", err)
	}

	if filepath.Dir(filepath.Dir(targetPath)) != filepath.Join(dungeonPath, "archived") {
		t.Fatalf("targetPath = %q, want dated bucket under campaign-root dungeon", targetPath)
	}
	if _, err := os.Stat(filepath.Join(root, "festivals", "dungeon", "archived")); !os.IsNotExist(err) {
		t.Fatalf("festivals/dungeon/archived should not be created, stat err = %v", err)
	}
}

func TestDungeonMove_RejectsFestivalsTriage(t *testing.T) {
	ctx := context.Background()

	root := t.TempDir()
	dungeonPath := filepath.Join(root, "dungeon")
	svc := NewService(root, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	festivalsDir := filepath.Join(root, "festivals")
	if err := os.MkdirAll(filepath.Join(festivalsDir, "active"), 0755); err != nil {
		t.Fatalf("failed to create festivals/active: %v", err)
	}
	docsDest := filepath.Join(root, "docs", "archive")
	if err := os.MkdirAll(docsDest, 0755); err != nil {
		t.Fatalf("failed to create docs destination: %v", err)
	}

	if err := svc.MoveToDungeon(ctx, "festivals", root); !errors.Is(err, ErrNotInDungeon) {
		t.Fatalf("MoveToDungeon() error = %v, want ErrNotInDungeon", err)
	}
	if _, err := svc.MoveToDungeonStatus(ctx, "festivals", root, "archived"); !errors.Is(err, ErrNotInDungeon) {
		t.Fatalf("MoveToDungeonStatus() error = %v, want ErrNotInDungeon", err)
	}
	if _, err := svc.MoveToDocs(ctx, "festivals", root, "archive"); !errors.Is(err, ErrNotInDungeon) {
		t.Fatalf("MoveToDocs() error = %v, want ErrNotInDungeon", err)
	}
	if _, err := os.Stat(festivalsDir); err != nil {
		t.Fatalf("festivals should remain after rejected moves: %v", err)
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
	_, err = svc.MoveToDungeonStatus(ctx, "item.txt", tmpDir, "../escape")
	if err == nil {
		t.Fatal("MoveToDungeonStatus should fail for path traversal")
	}
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got: %v", err)
	}

	// Empty status should be rejected
	_, err = svc.MoveToDungeonStatus(ctx, "item.txt", tmpDir, "")
	if err == nil {
		t.Fatal("MoveToDungeonStatus should fail for empty status")
	}
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got: %v", err)
	}
}

func TestService_MoveToDungeonStatus_ParentPathTraversal(t *testing.T) {
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

	// Create a file outside campaign root to ensure it's not moved
	outsideDir := filepath.Join(tmpDir, "..", "outside-campaign")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("failed to create outside dir: %v", err)
	}
	defer os.RemoveAll(outsideDir)

	if err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	// Attempt path traversal via parentPath
	_, err = svc.MoveToDungeonStatus(ctx, "secret.txt", "../../outside-campaign", "archived")
	if err == nil {
		t.Fatal("MoveToDungeonStatus should reject parentPath traversal")
	}
	if !errors.Is(err, ErrNotInDungeon) {
		t.Errorf("expected ErrNotInDungeon, got: %v", err)
	}
}

func TestService_MoveToDungeonStatus_ParentPathAbsolute(t *testing.T) {
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

	// Create a file outside campaign root
	outsideDir, err := os.MkdirTemp("", "outside-campaign-*")
	if err != nil {
		t.Fatalf("failed to create outside dir: %v", err)
	}
	defer os.RemoveAll(outsideDir)

	if err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	// Attempt with absolute path outside campaign root
	_, err = svc.MoveToDungeonStatus(ctx, "secret.txt", outsideDir, "archived")
	if err == nil {
		t.Fatal("MoveToDungeonStatus should reject absolute parentPath outside campaign root")
	}
	if !errors.Is(err, ErrNotInDungeon) {
		t.Errorf("expected ErrNotInDungeon, got: %v", err)
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

	archivedFile := filepath.Join(dungeonPath, "archived", time.Now().Format("2006-01-02"), "to-archive.txt")
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

func TestValidateDirectChildItemName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "simple file", input: "note.md", want: "note.md"},
		{name: "simple directory name", input: "legacy-folder", want: "legacy-folder"},
		{name: "trimmed whitespace", input: "  note.md  ", want: "note.md"},
		{name: "empty", input: "", wantErr: true},
		{name: "whitespace only", input: "   ", wantErr: true},
		{name: "parent traversal", input: "../secret.md", wantErr: true},
		{name: "nested path", input: "dir/file.md", wantErr: true},
		{name: "dot slash", input: "./note.md", wantErr: true},
		{name: "dot", input: ".", wantErr: true},
		{name: "dot dot", input: "..", wantErr: true},
		{name: "absolute path", input: "/tmp/note.md", wantErr: true},
		{name: "windows style separators", input: "dir\\note.md", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateDirectChildItemName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("validateDirectChildItemName(%q) expected error", tt.input)
				}
				if !errors.Is(err, ErrInvalidItemPath) {
					t.Fatalf("expected ErrInvalidItemPath, got: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("validateDirectChildItemName(%q) failed: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("validateDirectChildItemName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestService_MoveToDungeon_InvalidItemPath(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "safe.md"), []byte("safe"), 0o644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	for _, itemName := range []string{"../safe.md", "dir/safe.md", "./safe.md", `dir\safe.md`} {
		t.Run(itemName, func(t *testing.T) {
			err := svc.MoveToDungeon(ctx, itemName, tmpDir)
			if err == nil {
				t.Fatalf("MoveToDungeon(%q) expected invalid item path error", itemName)
			}
			if !errors.Is(err, ErrInvalidItemPath) {
				t.Fatalf("expected ErrInvalidItemPath, got: %v", err)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "safe.md")); err != nil {
		t.Fatalf("source file should remain in parent after failed move: %v", err)
	}
}

func TestService_MoveToStatus_InvalidItemPath(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dungeonPath, "safe.md"), []byte("safe"), 0o644); err != nil {
		t.Fatalf("failed to create dungeon file: %v", err)
	}

	for _, itemName := range []string{"../safe.md", "dir/safe.md", "./safe.md", `dir\safe.md`} {
		t.Run(itemName, func(t *testing.T) {
			_, err := svc.MoveToStatus(ctx, itemName, "completed")
			if err == nil {
				t.Fatalf("MoveToStatus(%q) expected invalid item path error", itemName)
			}
			if !errors.Is(err, ErrInvalidItemPath) {
				t.Fatalf("expected ErrInvalidItemPath, got: %v", err)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(dungeonPath, "safe.md")); err != nil {
		t.Fatalf("source file should remain in dungeon root after failed move: %v", err)
	}
}

func TestService_MoveToDungeonStatus_InvalidItemPath(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "safe.md"), []byte("safe"), 0o644); err != nil {
		t.Fatalf("failed to create parent file: %v", err)
	}

	for _, itemName := range []string{"../safe.md", "dir/safe.md", "./safe.md", `dir\safe.md`} {
		t.Run(itemName, func(t *testing.T) {
			_, err := svc.MoveToDungeonStatus(ctx, itemName, tmpDir, "archived")
			if err == nil {
				t.Fatalf("MoveToDungeonStatus(%q) expected invalid item path error", itemName)
			}
			if !errors.Is(err, ErrInvalidItemPath) {
				t.Fatalf("expected ErrInvalidItemPath, got: %v", err)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "safe.md")); err != nil {
		t.Fatalf("source file should remain in parent after failed move: %v", err)
	}
}

func assertDungeonFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s content = %q, want %q", path, data, want)
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

func TestService_MoveToStatus_ExternalLinkRewriteFlowsIntoCommit(t *testing.T) {
	root := t.TempDir()
	dungeonPath := filepath.Join(root, "dungeon")
	if err := os.MkdirAll(dungeonPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dungeonPath, "myitem.md"), []byte("# Item\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "index.md"), []byte("see [item](../dungeon/myitem.md)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewService(root, dungeonPath)
	dst, err := svc.MoveToStatus(context.Background(), "myitem.md", "completed")
	if err != nil {
		t.Fatalf("MoveToStatus: %v", err)
	}

	rewritten := svc.RewrittenLinkFiles()
	if !sliceContains(rewritten, "notes/index.md") {
		t.Fatalf("RewrittenLinkFiles() = %v, want it to include notes/index.md", rewritten)
	}

	relDst, err := filepath.Rel(root, dst)
	if err != nil {
		t.Fatal(err)
	}
	summary := &CrawlSummary{MovedItems: map[string][]string{"completed": {filepath.ToSlash(relDst)}}}
	paths := CrawlCommitPaths("dungeon", rewritten, summary)
	if !sliceContains(paths, "notes/index.md") {
		t.Fatalf("CrawlCommitPaths() = %v, want it to include the rewritten external file notes/index.md", paths)
	}
}

func TestService_BeginLinkBatch_DefersExternalRewriteUntilFlush(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dungeonPath := filepath.Join(root, "dungeon")
	svc := NewService(root, dungeonPath)
	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	for _, name := range []string{"alpha.md", "beta.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("# "+name+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	refA := filepath.Join(root, "notes", "a.md")
	refB := filepath.Join(root, "notes", "b.md")
	if err := os.WriteFile(refA, []byte("[a](../alpha.md)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refB, []byte("[b](../beta.md)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc.BeginLinkBatch()
	dstA, err := svc.MoveToDungeonStatus(ctx, "alpha.md", root, "completed")
	if err != nil {
		t.Fatalf("move alpha: %v", err)
	}
	if _, err := svc.MoveToDungeonStatus(ctx, "beta.md", root, "completed"); err != nil {
		t.Fatalf("move beta: %v", err)
	}

	if got := readFileContent(t, refA); got != "[a](../alpha.md)\n" {
		t.Fatalf("refA rewritten before flush: %q", got)
	}
	if got := readFileContent(t, refB); got != "[b](../beta.md)\n" {
		t.Fatalf("refB rewritten before flush: %q", got)
	}
	if files := svc.RewrittenLinkFiles(); sliceContains(files, "notes/a.md") || sliceContains(files, "notes/b.md") {
		t.Fatalf("external files recorded before flush: %v", files)
	}

	if err := svc.FlushLinkRewrites(ctx); err != nil {
		t.Fatalf("FlushLinkRewrites: %v", err)
	}

	relA, err := filepath.Rel(root, dstA)
	if err != nil {
		t.Fatal(err)
	}
	wantA := "[a](../" + filepath.ToSlash(relA) + ")\n"
	if got := readFileContent(t, refA); got != wantA {
		t.Fatalf("refA after flush: got %q, want %q", got, wantA)
	}
	files := svc.RewrittenLinkFiles()
	if !sliceContains(files, "notes/a.md") || !sliceContains(files, "notes/b.md") {
		t.Fatalf("RewrittenLinkFiles after flush = %v, want notes/a.md and notes/b.md", files)
	}
}

func readFileContent(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}

func sliceContains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
