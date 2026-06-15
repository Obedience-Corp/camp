package statusmove

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestMoveFileNoReplace(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source.md")
	dst := filepath.Join(root, "dest", "source.md")
	if err := os.WriteFile(src, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := Move(context.Background(), src, dst, MoveOptions{})
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	if got != dst {
		t.Fatalf("Move() destination = %q, want %q", got, dst)
	}
	assertFile(t, dst, "source")
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source should be removed, stat err = %v", err)
	}
}

func TestMoveDirectoryNoReplace(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "item.md"), []byte("item"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "dest", "source")

	if _, err := Move(context.Background(), src, dst, MoveOptions{}); err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	assertFile(t, filepath.Join(dst, "nested", "item.md"), "item")
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source should be removed, stat err = %v", err)
	}
}

func TestMoveCollisionDoesNotReplace(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source.md")
	dst := filepath.Join(root, "dest.md")
	if err := os.WriteFile(src, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Move(context.Background(), src, dst, MoveOptions{})
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("Move() error = %v, want ErrAlreadyExists", err)
	}
	assertFile(t, src, "source")
	assertFile(t, dst, "existing")
}

func TestMoveDatedBucket(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source.md")
	statusRoot := filepath.Join(root, "archived")
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	if err := os.WriteFile(src, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := Move(context.Background(), src, statusRoot, MoveOptions{
		DatedBucket: true,
		Now:         &now,
	})
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	want := filepath.Join(statusRoot, "2026-06-15", "source.md")
	if got != want {
		t.Fatalf("Move() destination = %q, want %q", got, want)
	}
	assertFile(t, want, "source")
}

func TestMoveDatedBucketLogicalCollision(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source.md")
	statusRoot := filepath.Join(root, "archived")
	existing := filepath.Join(statusRoot, "2026-06-14", "source.md")
	if err := os.MkdirAll(filepath.Dir(existing), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Move(context.Background(), src, statusRoot, MoveOptions{DatedBucket: true})
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("Move() error = %v, want ErrAlreadyExists", err)
	}
	assertFile(t, src, "source")
	assertFile(t, existing, "existing")
}

func TestMoveBoundary(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source.md")
	outside := filepath.Join(t.TempDir(), "source.md")
	if err := os.WriteFile(src, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Move(context.Background(), src, outside, MoveOptions{BoundaryRoot: root})
	if err == nil {
		t.Fatal("Move() expected boundary error")
	}
	assertFile(t, src, "source")
}

func TestMoveMissingSource(t *testing.T) {
	root := t.TempDir()
	_, err := Move(context.Background(), filepath.Join(root, "missing.md"), filepath.Join(root, "dest.md"), MoveOptions{})
	if !errors.Is(err, camperrors.ErrNotFound) {
		t.Fatalf("Move() error = %v, want ErrNotFound", err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s content = %q, want %q", path, data, want)
	}
}
