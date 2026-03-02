package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestAdd_EmptyRemote_PreflightError(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()
	campaignRoot, _ = filepath.EvalSymlinks(campaignRoot)
	initGitRepo(t, campaignRoot)
	initGitRepoWithCommit(t, campaignRoot) // give campaign root a commit

	// Create an empty remote (no commits).
	emptyRemote := t.TempDir()
	emptyRemote, _ = filepath.EvalSymlinks(emptyRemote)
	initGitRepo(t, emptyRemote)

	_, err := Add(ctx, campaignRoot, emptyRemote, AddOptions{})
	if err == nil {
		t.Fatal("expected error for empty repo, got nil")
	}
	if !strings.Contains(err.Error(), "cannot add empty repository") {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestAdd_FailureCleanup_NoResidualState(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()
	campaignRoot, _ = filepath.EvalSymlinks(campaignRoot)
	initGitRepo(t, campaignRoot)
	initGitRepoWithCommit(t, campaignRoot)

	emptyRemote := t.TempDir()
	emptyRemote, _ = filepath.EvalSymlinks(emptyRemote)
	initGitRepo(t, emptyRemote)

	name := "empty-test"
	_, _ = Add(ctx, campaignRoot, emptyRemote, AddOptions{Name: name})

	// Project directory must not exist.
	projectDir := filepath.Join(campaignRoot, "projects", name)
	if _, err := os.Stat(projectDir); !os.IsNotExist(err) {
		t.Errorf("expected projects/%s to be absent after failure, got: %v", name, err)
	}

	// .git/modules entry must not exist.
	modulesDir := filepath.Join(campaignRoot, ".git", "modules", "projects", name)
	if _, err := os.Stat(modulesDir); !os.IsNotExist(err) {
		t.Errorf("expected .git/modules/projects/%s to be absent, got: %v", name, err)
	}

	// .gitmodules must not have a section for this submodule.
	gitmodulesPath := filepath.Join(campaignRoot, ".gitmodules")
	data, readErr := os.ReadFile(gitmodulesPath)
	if readErr == nil {
		if strings.Contains(string(data), name) {
			t.Errorf(".gitmodules still contains reference to %q after cleanup:\n%s", name, data)
		}
	}
}

func TestAdd_RetryAfterCleanup_Succeeds(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()
	campaignRoot, _ = filepath.EvalSymlinks(campaignRoot)
	initGitRepo(t, campaignRoot)
	initGitRepoWithCommit(t, campaignRoot)

	// First: empty remote (fails).
	emptyRemote := t.TempDir()
	emptyRemote, _ = filepath.EvalSymlinks(emptyRemote)
	initGitRepo(t, emptyRemote)

	name := "retry-project"
	_, _ = Add(ctx, campaignRoot, emptyRemote, AddOptions{Name: name})

	// Now populate the remote with a commit.
	cmd := exec.Command("git", "-C", emptyRemote, "config", "user.email", "test@test.com")
	cmd.Run()
	cmd = exec.Command("git", "-C", emptyRemote, "config", "user.name", "Test")
	cmd.Run()
	os.WriteFile(filepath.Join(emptyRemote, "main.go"), []byte("package main"), 0644)
	cmd = exec.Command("git", "-C", emptyRemote, "add", ".")
	cmd.Run()
	cmd = exec.Command("git", "-C", emptyRemote, "commit", "-m", "initial commit")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to commit in remote: %v", err)
	}

	// Retry with the same name via Local path — must succeed cleanly.
	// (Uses Local because file:// protocol requires protocol.file.allow=always
	// which only addLocalAsSubmodule sets.)
	result, err := Add(ctx, campaignRoot, "unused", AddOptions{
		Name:  name,
		Local: emptyRemote,
	})
	if err != nil {
		t.Fatalf("Add after cleanup failed: %v", err)
	}
	if result.Name != name {
		t.Errorf("result.Name = %q, want %q", result.Name, name)
	}
}

func TestAdd_NonEmptyLocal_Succeeds(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()
	campaignRoot, _ = filepath.EvalSymlinks(campaignRoot)
	initGitRepo(t, campaignRoot)
	initGitRepoWithCommit(t, campaignRoot)

	remote := t.TempDir()
	remote, _ = filepath.EvalSymlinks(remote)
	initGitRepoWithCommit(t, remote)

	result, err := Add(ctx, campaignRoot, remote, AddOptions{
		Local: remote,
	})
	if err != nil {
		t.Fatalf("Add non-empty repo failed: %v", err)
	}
	if result.Name == "" {
		t.Error("result.Name is empty")
	}
	// Project directory must exist.
	projectDir := filepath.Join(campaignRoot, result.Path)
	if _, err := os.Stat(projectDir); err != nil {
		t.Errorf("project directory %s not created: %v", projectDir, err)
	}
}

// TestAdd_InvalidURL tests URL validation.
func TestAdd_InvalidURL(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Initialize as git repo
	initGitRepo(t, tmpDir)

	ctx := context.Background()
	_, err := Add(ctx, tmpDir, "not-a-valid-url", AddOptions{})

	if err == nil {
		t.Fatal("Add() should return error for invalid URL")
	}

	if !strings.Contains(err.Error(), "invalid git URL format") {
		t.Errorf("error should mention invalid URL format, got: %v", err)
	}
}

// TestAdd_NotInGitRepo tests the git repo pre-flight check.
func TestAdd_NotInGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	// Don't initialize as git repo

	ctx := context.Background()
	_, err := Add(ctx, tmpDir, "git@github.com:org/repo.git", AddOptions{})

	if err == nil {
		t.Fatal("Add() should return error when not in a git repo")
	}

	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error should mention not a git repository, got: %v", err)
	}
}

// TestCheckGitInstalled tests the git installation check.
func TestCheckGitInstalled(t *testing.T) {
	ctx := context.Background()
	err := checkGitInstalled(ctx)

	// This should pass on any system with git installed
	if err != nil {
		t.Skipf("git not installed on this system: %v", err)
	}
}

// TestCheckIsGitRepo tests the git repo check.
func TestCheckIsGitRepo(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(t *testing.T) string
		expectError bool
	}{
		{
			name: "valid git repo",
			setupFunc: func(t *testing.T) string {
				tmpDir := t.TempDir()
				initGitRepo(t, tmpDir)
				return tmpDir
			},
			expectError: false,
		},
		{
			name: "not a git repo",
			setupFunc: func(t *testing.T) string {
				return t.TempDir()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setupFunc(t)
			ctx := context.Background()
			err := checkIsGitRepo(ctx, path)

			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
