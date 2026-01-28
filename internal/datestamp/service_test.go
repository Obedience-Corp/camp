package datestamp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDatestamp(t *testing.T) {
	t.Run("renames file with extension", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.md")
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}

		result, err := Datestamp(context.Background(), path, Options{
			DateFormat: "2006-01-02",
		})
		if err != nil {
			t.Fatal(err)
		}

		if !result.Executed {
			t.Error("expected Executed to be true")
		}

		expectedSuffix := time.Now().Format("2006-01-02")
		expectedName := "test-" + expectedSuffix + ".md"
		if filepath.Base(result.NewPath) != expectedName {
			t.Errorf("expected %s, got %s", expectedName, filepath.Base(result.NewPath))
		}

		// Verify file was actually renamed
		if _, err := os.Stat(result.NewPath); err != nil {
			t.Error("renamed file does not exist")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("original file still exists")
		}
	})

	t.Run("renames directory", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mydir")
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}

		result, err := Datestamp(context.Background(), path, Options{})
		if err != nil {
			t.Fatal(err)
		}

		if !result.IsDirectory {
			t.Error("expected IsDirectory to be true")
		}

		expectedSuffix := time.Now().Format("2006-01-02")
		expectedName := "mydir-" + expectedSuffix
		if filepath.Base(result.NewPath) != expectedName {
			t.Errorf("expected %s, got %s", expectedName, filepath.Base(result.NewPath))
		}
	})

	t.Run("uses custom date format", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}

		result, err := Datestamp(context.Background(), path, Options{
			DateFormat: "20060102",
		})
		if err != nil {
			t.Fatal(err)
		}

		expectedSuffix := time.Now().Format("20060102")
		expectedName := "file-" + expectedSuffix + ".txt"
		if filepath.Base(result.NewPath) != expectedName {
			t.Errorf("expected %s, got %s", expectedName, filepath.Base(result.NewPath))
		}
	})

	t.Run("uses days ago", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "old.md")
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}

		result, err := Datestamp(context.Background(), path, Options{
			DaysAgo: 5,
		})
		if err != nil {
			t.Fatal(err)
		}

		expectedDate := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
		expectedName := "old-" + expectedDate + ".md"
		if filepath.Base(result.NewPath) != expectedName {
			t.Errorf("expected %s, got %s", expectedName, filepath.Base(result.NewPath))
		}
	})

	t.Run("uses mtime when specified", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "modified.txt")
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}

		// Set a specific modification time
		mtime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}

		result, err := Datestamp(context.Background(), path, Options{
			UseMtime: true,
		})
		if err != nil {
			t.Fatal(err)
		}

		expectedName := "modified-2024-06-15.txt"
		if filepath.Base(result.NewPath) != expectedName {
			t.Errorf("expected %s, got %s", expectedName, filepath.Base(result.NewPath))
		}

		if result.DateUsed.Format("2006-01-02") != "2024-06-15" {
			t.Errorf("expected DateUsed to be 2024-06-15, got %s", result.DateUsed.Format("2006-01-02"))
		}
	})

	t.Run("dry run does not rename", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "dryrun.txt")
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}

		result, err := Datestamp(context.Background(), path, Options{
			DryRun: true,
		})
		if err != nil {
			t.Fatal(err)
		}

		if result.Executed {
			t.Error("expected Executed to be false for dry run")
		}

		// Original file should still exist
		if _, err := os.Stat(path); err != nil {
			t.Error("original file should still exist after dry run")
		}

		// New path should NOT exist
		if _, err := os.Stat(result.NewPath); !os.IsNotExist(err) {
			t.Error("new path should not exist after dry run")
		}
	})

	t.Run("errors when path not found", func(t *testing.T) {
		_, err := Datestamp(context.Background(), "/nonexistent/path", Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' in error, got: %v", err)
		}
	})

	t.Run("errors when target already exists", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "source.txt")
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create the target that would conflict
		expectedSuffix := time.Now().Format("2006-01-02")
		target := filepath.Join(dir, "source-"+expectedSuffix+".txt")
		if err := os.WriteFile(target, []byte("existing"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := Datestamp(context.Background(), path, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("expected 'already exists' in error, got: %v", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := Datestamp(ctx, "/any/path", Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "context cancelled") {
			t.Errorf("expected 'context cancelled' in error, got: %v", err)
		}
	})

	t.Run("handles hidden files", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".gitignore")
		if err := os.WriteFile(path, []byte("*.log"), 0644); err != nil {
			t.Fatal(err)
		}

		result, err := Datestamp(context.Background(), path, Options{})
		if err != nil {
			t.Fatal(err)
		}

		expectedSuffix := time.Now().Format("2006-01-02")
		expectedName := ".gitignore-" + expectedSuffix
		if filepath.Base(result.NewPath) != expectedName {
			t.Errorf("expected %s, got %s", expectedName, filepath.Base(result.NewPath))
		}
	})
}

func TestBuildNewPath(t *testing.T) {
	date := time.Date(2026, 1, 27, 0, 0, 0, 0, time.UTC)
	format := "2006-01-02"

	tests := []struct {
		name     string
		path     string
		isDir    bool
		expected string
	}{
		{
			name:     "file with extension",
			path:     "/foo/bar/test.md",
			isDir:    false,
			expected: "/foo/bar/test-2026-01-27.md",
		},
		{
			name:     "file without extension",
			path:     "/foo/bar/README",
			isDir:    false,
			expected: "/foo/bar/README-2026-01-27",
		},
		{
			name:     "directory",
			path:     "/foo/bar/mydir",
			isDir:    true,
			expected: "/foo/bar/mydir-2026-01-27",
		},
		{
			name:     "hidden file",
			path:     "/foo/.gitignore",
			isDir:    false,
			expected: "/foo/.gitignore-2026-01-27",
		},
		{
			name:     "file with multiple dots",
			path:     "/foo/archive.tar.gz",
			isDir:    false,
			expected: "/foo/archive.tar-2026-01-27.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildNewPath(tt.path, format, date, tt.isDir)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}
