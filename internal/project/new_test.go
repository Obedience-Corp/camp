package project

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNew_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	initGitRepo(t, tmpDir)
	os.MkdirAll(filepath.Join(tmpDir, "projects"), 0755)

	ctx := context.Background()
	result, err := New(ctx, tmpDir, "my-project", NewOptions{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if result.Name != "my-project" {
		t.Errorf("result.Name = %q, want %q", result.Name, "my-project")
	}

	if result.Path != "projects/my-project" {
		t.Errorf("result.Path = %q, want %q", result.Path, "projects/my-project")
	}

	if result.Source != "local (new)" {
		t.Errorf("result.Source = %q, want %q", result.Source, "local (new)")
	}

	// Verify project directory exists
	projectPath := filepath.Join(tmpDir, "projects", "my-project")
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		t.Error("project directory should exist")
	}

	// Verify .git exists (submodule creates a .git file)
	gitPath := filepath.Join(projectPath, ".git")
	if _, err := os.Stat(gitPath); os.IsNotExist(err) {
		t.Error(".git should exist in project directory")
	}

	// Verify README exists
	readmePath := filepath.Join(projectPath, "README.md")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("failed to read README: %v", err)
	}
	if string(content) != "# my-project\n" {
		t.Errorf("README content = %q, want %q", string(content), "# my-project\n")
	}
}

func TestNew_CustomPath(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	initGitRepo(t, tmpDir)
	os.MkdirAll(filepath.Join(tmpDir, "libs"), 0755)

	ctx := context.Background()
	result, err := New(ctx, tmpDir, "my-lib", NewOptions{
		Path: "libs/my-lib",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if result.Path != "libs/my-lib" {
		t.Errorf("result.Path = %q, want %q", result.Path, "libs/my-lib")
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "libs", "my-lib")); os.IsNotExist(err) {
		t.Error("project directory should exist at custom path")
	}
}

func TestNew_ProjectExists(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	initGitRepo(t, tmpDir)

	// Create existing project directory
	os.MkdirAll(filepath.Join(tmpDir, "projects", "existing"), 0755)

	ctx := context.Background()
	_, err := New(ctx, tmpDir, "existing", NewOptions{})
	if err == nil {
		t.Fatal("New() should return error for existing project")
	}

	if _, ok := err.(*ErrProjectExists); !ok {
		t.Errorf("error type = %T, want *ErrProjectExists", err)
	}
}

func TestNew_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := New(ctx, "/some/path", "test", NewOptions{})
	if err != context.Canceled {
		t.Errorf("New() error = %v, want %v", err, context.Canceled)
	}
}

func TestNew_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := New(ctx, "/some/path", "test", NewOptions{})
	if err != context.DeadlineExceeded {
		t.Errorf("New() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestNew_NotInGitRepo(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := context.Background()
	_, err := New(ctx, tmpDir, "test", NewOptions{})
	if err == nil {
		t.Fatal("New() should return error when not in a git repo")
	}
}

func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name      string
		wantError bool
	}{
		{"my-project", false},
		{"my_project", false},
		{"project123", false},
		{"A-Project", false},
		{"a", false},
		{"", true},
		{".hidden", true},
		{"has space", true},
		{"has/slash", true},
		{"has\\backslash", true},
		{"../escape", true},
		{"-starts-with-dash", true},
		{"_starts-with-underscore", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProjectName(tt.name)
			if (err != nil) != tt.wantError {
				t.Errorf("validateProjectName(%q) error = %v, wantError = %v", tt.name, err, tt.wantError)
			}
		})
	}
}

func TestNew_CreatesWorktreeDir(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	initGitRepo(t, tmpDir)
	os.MkdirAll(filepath.Join(tmpDir, "projects"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "worktrees"), 0755)

	ctx := context.Background()
	result, err := New(ctx, tmpDir, "wt-test", NewOptions{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	worktreePath := filepath.Join(tmpDir, "worktrees", result.Name)
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Error("worktree directory should be created")
	}
}
