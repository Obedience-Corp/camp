package dungeon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDocsDestination(t *testing.T) {
	root, err := os.MkdirTemp("", "dungeon-docs-resolve-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(root)

	tests := []struct {
		name        string
		destination string
		wantPath    string
		wantErr     bool
	}{
		{
			name:        "valid subpath",
			destination: "architecture/api",
			wantPath:    filepath.Join(root, "docs", "architecture", "api"),
		},
		{
			name:        "valid docs-prefixed path",
			destination: "docs/architecture/api",
			wantPath:    filepath.Join(root, "docs", "architecture", "api"),
		},
		{
			name:        "empty destination",
			destination: "",
			wantErr:     true,
		},
		{
			name:        "absolute destination",
			destination: "/tmp/escape",
			wantErr:     true,
		},
		{
			name:        "traversal destination",
			destination: "../escape",
			wantErr:     true,
		},
		{
			name:        "docs root only not allowed",
			destination: "docs",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveDocsDestination(root, tt.destination)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveDocsDestination(%q) expected error", tt.destination)
				}
				if !errors.Is(err, ErrInvalidDocsDestination) {
					t.Fatalf("expected ErrInvalidDocsDestination, got: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("ResolveDocsDestination(%q) failed: %v", tt.destination, err)
			}
			if got != tt.wantPath {
				t.Fatalf("ResolveDocsDestination(%q) = %q, want %q", tt.destination, got, tt.wantPath)
			}
		})
	}
}

func TestService_MoveToDocs(t *testing.T) {
	ctx := context.Background()

	root, err := os.MkdirTemp("", "dungeon-docs-move-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(root)

	dungeonPath := filepath.Join(root, "dungeon")
	if err := os.MkdirAll(dungeonPath, 0o755); err != nil {
		t.Fatalf("failed to create dungeon dir: %v", err)
	}

	parentPath := filepath.Join(root, "workflow", "design")
	if err := os.MkdirAll(parentPath, 0o755); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	targetDir := filepath.Join(root, "docs", "architecture", "api")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("failed to create docs destination dir: %v", err)
	}

	source := filepath.Join(parentPath, "old-notes.md")
	if err := os.WriteFile(source, []byte("# Old Notes\n"), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	svc := NewService(root, dungeonPath)
	target, err := svc.MoveToDocs(ctx, "old-notes.md", parentPath, "architecture/api")
	if err != nil {
		t.Fatalf("MoveToDocs failed: %v", err)
	}

	wantTarget := filepath.Join(root, "docs", "architecture", "api", "old-notes.md")
	if target != wantTarget {
		t.Fatalf("MoveToDocs target = %q, want %q", target, wantTarget)
	}

	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source should be removed after move, stat err=%v", err)
	}
	if _, err := os.Stat(wantTarget); err != nil {
		t.Fatalf("target should exist after move: %v", err)
	}
}

func TestService_MoveToDocs_RequiresExistingDestination(t *testing.T) {
	ctx := context.Background()

	root, err := os.MkdirTemp("", "dungeon-docs-missing-destination-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(root)

	dungeonPath := filepath.Join(root, "dungeon")
	if err := os.MkdirAll(dungeonPath, 0o755); err != nil {
		t.Fatalf("failed to create dungeon dir: %v", err)
	}

	parentPath := filepath.Join(root, "workflow", "design")
	if err := os.MkdirAll(parentPath, 0o755); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("failed to create docs root: %v", err)
	}

	source := filepath.Join(parentPath, "old-notes.md")
	if err := os.WriteFile(source, []byte("# Old Notes\n"), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	svc := NewService(root, dungeonPath)
	_, err = svc.MoveToDocs(ctx, "old-notes.md", parentPath, "architecture/api")
	if err == nil {
		t.Fatal("MoveToDocs expected destination existence error")
	}
	if !errors.Is(err, ErrInvalidDocsDestination) {
		t.Fatalf("expected ErrInvalidDocsDestination, got: %v", err)
	}

	if _, statErr := os.Stat(source); statErr != nil {
		t.Fatalf("source should remain in place after failed move: %v", statErr)
	}
}

func TestService_MoveToDocs_InvalidDestination(t *testing.T) {
	ctx := context.Background()

	root, err := os.MkdirTemp("", "dungeon-docs-invalid-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(root)

	dungeonPath := filepath.Join(root, "dungeon")
	if err := os.MkdirAll(dungeonPath, 0o755); err != nil {
		t.Fatalf("failed to create dungeon dir: %v", err)
	}

	parentPath := filepath.Join(root, "workflow", "design")
	if err := os.MkdirAll(parentPath, 0o755); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}

	source := filepath.Join(parentPath, "old-notes.md")
	if err := os.WriteFile(source, []byte("# Old Notes\n"), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	svc := NewService(root, dungeonPath)
	_, err = svc.MoveToDocs(ctx, "old-notes.md", parentPath, "../escape")
	if err == nil {
		t.Fatal("MoveToDocs expected invalid destination error")
	}
	if !errors.Is(err, ErrInvalidDocsDestination) {
		t.Fatalf("expected ErrInvalidDocsDestination, got: %v", err)
	}

	if _, statErr := os.Stat(source); statErr != nil {
		t.Fatalf("source should remain in place after failed move: %v", statErr)
	}
}
