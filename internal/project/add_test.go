package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"git@github.com:org/repo.git", "repo"},
		{"git@github.com:org/my-project.git", "my-project"},
		{"https://github.com/org/repo.git", "repo"},
		{"https://github.com/org/my-project.git", "my-project"},
		{"https://github.com/org/repo", "repo"},
		{"git@gitlab.com:group/subgroup/repo.git", "repo"},
		{"/path/to/local/repo", "repo"},
		{"/path/to/local/repo.git", "repo"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractRepoName(tt.url)
			if got != tt.expected {
				t.Errorf("extractRepoName(%q) = %q, want %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestAdd_ProjectExists(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Initialize as git repo
	initGitRepo(t, tmpDir)

	// Create existing project
	projectsDir := filepath.Join(tmpDir, "projects")
	projectPath := filepath.Join(projectsDir, "existing")
	os.MkdirAll(projectPath, 0755)

	ctx := context.Background()
	_, err := Add(ctx, tmpDir, "git@github.com:org/existing.git", AddOptions{})

	if err == nil {
		t.Fatal("Add() should return error for existing project")
	}

	var exists *ErrProjectExists
	if _, ok := err.(*ErrProjectExists); !ok {
		t.Errorf("error type = %T, want *ErrProjectExists", err)
	} else {
		exists = err.(*ErrProjectExists)
		if exists.Name != "existing" {
			t.Errorf("ErrProjectExists.Name = %q, want %q", exists.Name, "existing")
		}
	}
}

func TestAdd_CustomName(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Initialize as git repo
	initGitRepo(t, tmpDir)

	// Create a local repo to add
	localRepo := filepath.Join(tmpDir, "local-source")
	initGitRepoWithCommit(t, localRepo)

	// Create projects dir
	os.MkdirAll(filepath.Join(tmpDir, "projects"), 0755)

	ctx := context.Background()
	result, err := Add(ctx, tmpDir, "unused", AddOptions{
		Name:  "custom-name",
		Local: localRepo,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if result.Name != "custom-name" {
		t.Errorf("result.Name = %q, want %q", result.Name, "custom-name")
	}

	if result.Path != "projects/custom-name" {
		t.Errorf("result.Path = %q, want %q", result.Path, "projects/custom-name")
	}
}

func TestAdd_CreatesWorktreeDir(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Initialize as git repo
	initGitRepo(t, tmpDir)

	// Create a local repo to add
	localRepo := filepath.Join(tmpDir, "local-source")
	initGitRepoWithCommit(t, localRepo)

	// Create required dirs
	os.MkdirAll(filepath.Join(tmpDir, "projects"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "worktrees"), 0755)

	ctx := context.Background()
	result, err := Add(ctx, tmpDir, "unused", AddOptions{
		Name:  "test-project",
		Local: localRepo,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Verify worktree directory was created
	worktreePath := filepath.Join(tmpDir, "worktrees", result.Name)
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Error("worktree directory should be created")
	}
}

func TestAdd_LocalNotGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Initialize campaign as git repo
	initGitRepo(t, tmpDir)

	// Create a local directory that's NOT a git repo
	localDir := filepath.Join(tmpDir, "not-a-repo")
	os.MkdirAll(localDir, 0755)

	// Create projects dir
	os.MkdirAll(filepath.Join(tmpDir, "projects"), 0755)

	ctx := context.Background()
	_, err := Add(ctx, tmpDir, "unused", AddOptions{
		Local: localDir,
	})

	if err == nil {
		t.Fatal("Add() should return error for non-git local directory")
	}
}

func TestAdd_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Add(ctx, "/some/path", "git@github.com:org/repo.git", AddOptions{})
	if err != context.Canceled {
		t.Errorf("Add() error = %v, want %v", err, context.Canceled)
	}
}

func TestAdd_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := Add(ctx, "/some/path", "git@github.com:org/repo.git", AddOptions{})
	if err != context.DeadlineExceeded {
		t.Errorf("Add() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestErrProjectExists_Error(t *testing.T) {
	err := &ErrProjectExists{Name: "test", Path: "projects/test"}
	expected := "project already exists: test at projects/test"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestAdd_LocalRepo(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Initialize campaign as git repo
	initGitRepo(t, tmpDir)

	// Create a local repo to add with go.mod committed
	localRepo := filepath.Join(tmpDir, "local-source")
	initGitRepoWithGoMod(t, localRepo)

	// Create required dirs
	os.MkdirAll(filepath.Join(tmpDir, "projects"), 0755)

	ctx := context.Background()
	result, err := Add(ctx, tmpDir, "unused", AddOptions{
		Local: localRepo,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Verify result
	if result.Name != "local-source" {
		t.Errorf("result.Name = %q, want %q", result.Name, "local-source")
	}

	if result.Type != TypeGo {
		t.Errorf("result.Type = %q, want %q", result.Type, TypeGo)
	}

	// Verify project was added
	projectPath := filepath.Join(tmpDir, "projects", "local-source")
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		t.Error("project directory should exist")
	}
}

// Helper to initialize a git repo with a commit
func initGitRepoWithCommit(t *testing.T, path string) {
	t.Helper()
	os.MkdirAll(path, 0755)

	cmd := exec.Command("git", "init", path)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for the test
	cmd = exec.Command("git", "-C", path, "config", "user.email", "test@test.com")
	cmd.Run()
	cmd = exec.Command("git", "-C", path, "config", "user.name", "Test")
	cmd.Run()

	// Create initial commit
	readmePath := filepath.Join(path, "README.md")
	os.WriteFile(readmePath, []byte("# Test"), 0644)

	cmd = exec.Command("git", "-C", path, "add", ".")
	cmd.Run()

	cmd = exec.Command("git", "-C", path, "commit", "-m", "Initial commit")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}
}

// Helper to initialize a git repo with go.mod committed
func initGitRepoWithGoMod(t *testing.T, path string) {
	t.Helper()
	os.MkdirAll(path, 0755)

	cmd := exec.Command("git", "init", path)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for the test
	cmd = exec.Command("git", "-C", path, "config", "user.email", "test@test.com")
	cmd.Run()
	cmd = exec.Command("git", "-C", path, "config", "user.name", "Test")
	cmd.Run()

	// Create go.mod
	goModPath := filepath.Join(path, "go.mod")
	os.WriteFile(goModPath, []byte("module test\n\ngo 1.21\n"), 0644)

	// Create README
	readmePath := filepath.Join(path, "README.md")
	os.WriteFile(readmePath, []byte("# Test Go Project"), 0644)

	cmd = exec.Command("git", "-C", path, "add", ".")
	cmd.Run()

	cmd = exec.Command("git", "-C", path, "commit", "-m", "Initial commit with go.mod")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}
}
