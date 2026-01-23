package git

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	exec.Command("git", "init", tmpDir).Run()
	exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run()
	return tmpDir
}

func TestNewExecutor(t *testing.T) {
	tmpDir := setupGitRepo(t)

	exec, err := NewExecutor(tmpDir)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	if exec.Path() != tmpDir {
		t.Errorf("Path() = %v, want %v", exec.Path(), tmpDir)
	}
}

func TestNewExecutor_NotARepo(t *testing.T) {
	tmpDir := t.TempDir() // Not a git repo

	_, err := NewExecutor(tmpDir)
	if err == nil {
		t.Error("NewExecutor() error = nil, want error for non-repo path")
	}
}

func TestNewExecutor_FromNestedPath(t *testing.T) {
	tmpDir := setupGitRepo(t)
	nestedDir := filepath.Join(tmpDir, "a", "b", "c")
	os.MkdirAll(nestedDir, 0755)

	exec, err := NewExecutor(nestedDir)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	if exec.Path() != tmpDir {
		t.Errorf("Path() = %v, want %v", exec.Path(), tmpDir)
	}
}

func TestNewExecutor_WithLogger(t *testing.T) {
	tmpDir := setupGitRepo(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	exec, err := NewExecutor(tmpDir, WithLogger(logger))
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	if exec.Logger() != logger {
		t.Error("WithLogger() did not set logger")
	}
}

func TestNewExecutor_WithConfig(t *testing.T) {
	tmpDir := setupGitRepo(t)
	cfg := &ExecutorConfig{
		AutoStage:  false,
		MaxRetries: 5,
		Verbose:    true,
	}

	exec, err := NewExecutor(tmpDir, WithConfig(cfg))
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	if exec.Config().AutoStage != false {
		t.Error("Config.AutoStage should be false")
	}
	if exec.Config().MaxRetries != 5 {
		t.Errorf("Config.MaxRetries = %d, want 5", exec.Config().MaxRetries)
	}
	if exec.Config().Verbose != true {
		t.Error("Config.Verbose should be true")
	}
}

func TestDefaultExecutorConfig(t *testing.T) {
	cfg := DefaultExecutorConfig()

	if !cfg.AutoStage {
		t.Error("AutoStage should default to true")
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.Verbose {
		t.Error("Verbose should default to false")
	}
}

func TestExecutor_CommitAll(t *testing.T) {
	tmpDir := setupGitRepo(t)
	exec, _ := NewExecutor(tmpDir)

	// Create a file
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)

	ctx := context.Background()
	err := exec.CommitAll(ctx, "test commit")
	if err != nil {
		t.Fatalf("CommitAll() error = %v", err)
	}

	// Verify no uncommitted changes
	hasChanges, _ := exec.HasChanges(ctx)
	if hasChanges {
		t.Error("HasChanges() = true after commit")
	}
}

func TestExecutor_Commit(t *testing.T) {
	tmpDir := setupGitRepo(t)
	exec, _ := NewExecutor(tmpDir)

	// Create and stage a file
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)
	exec.StageAll(context.Background())

	ctx := context.Background()
	err := exec.Commit(ctx, &CommitOptions{Message: "test commit"})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
}

func TestExecutor_Stage(t *testing.T) {
	tmpDir := setupGitRepo(t)
	exec, _ := NewExecutor(tmpDir)

	// Create files
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0644)

	ctx := context.Background()
	err := exec.Stage(ctx, []string{"a.txt"})
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
}

func TestExecutor_StageAll(t *testing.T) {
	tmpDir := setupGitRepo(t)
	exec, _ := NewExecutor(tmpDir)

	// Create files
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)

	ctx := context.Background()
	err := exec.StageAll(ctx)
	if err != nil {
		t.Fatalf("StageAll() error = %v", err)
	}
}

func TestExecutor_HasChanges(t *testing.T) {
	t.Run("no changes", func(t *testing.T) {
		tmpDir := setupGitRepo(t)
		exec, _ := NewExecutor(tmpDir)

		ctx := context.Background()
		hasChanges, err := exec.HasChanges(ctx)
		if err != nil {
			t.Fatalf("HasChanges() error = %v", err)
		}
		if hasChanges {
			t.Error("HasChanges() = true for empty repo")
		}
	})

	t.Run("with untracked files", func(t *testing.T) {
		tmpDir := setupGitRepo(t)
		exec, _ := NewExecutor(tmpDir)

		os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)

		ctx := context.Background()
		hasChanges, err := exec.HasChanges(ctx)
		if err != nil {
			t.Fatalf("HasChanges() error = %v", err)
		}
		if !hasChanges {
			t.Error("HasChanges() = false with untracked files")
		}
	})

	t.Run("with staged changes", func(t *testing.T) {
		tmpDir := setupGitRepo(t)
		exec, _ := NewExecutor(tmpDir)

		os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)
		exec.StageAll(context.Background())

		ctx := context.Background()
		hasChanges, err := exec.HasChanges(ctx)
		if err != nil {
			t.Fatalf("HasChanges() error = %v", err)
		}
		if !hasChanges {
			t.Error("HasChanges() = false with staged changes")
		}
	})
}

func TestExecutor_CleanLocks(t *testing.T) {
	tmpDir := setupGitRepo(t)
	exec, _ := NewExecutor(tmpDir)

	// Create a stale lock
	lockPath := filepath.Join(tmpDir, ".git", "index.lock")
	os.WriteFile(lockPath, []byte{}, 0644)

	ctx := context.Background()
	result, err := exec.CleanLocks(ctx)
	if err != nil {
		t.Fatalf("CleanLocks() error = %v", err)
	}

	if len(result.Removed) != 1 {
		t.Errorf("Removed %d locks, want 1", len(result.Removed))
	}

	// Verify lock is gone
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("Lock still exists after CleanLocks()")
	}
}

func TestExecutor_CommitAll_NoAutoStage(t *testing.T) {
	tmpDir := setupGitRepo(t)
	cfg := &ExecutorConfig{
		AutoStage:  false,
		MaxRetries: 3,
	}
	exec, _ := NewExecutor(tmpDir, WithConfig(cfg))

	// Create a file but don't stage it
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)

	ctx := context.Background()
	err := exec.CommitAll(ctx, "test commit")
	// Should fail because nothing is staged
	if err == nil {
		t.Error("CommitAll() should fail with AutoStage=false and no staged changes")
	}
}

func TestExecutor_ImplementsInterface(t *testing.T) {
	tmpDir := setupGitRepo(t)
	exec, _ := NewExecutor(tmpDir)

	// Verify the executor implements the interface
	var _ GitExecutor = exec
}

// MockExecutor for testing commands
type MockExecutor struct {
	CommitCalled  bool
	CommitMessage string
	StagedFiles   []string
	ReturnError   error
	ChangesExist  bool
	path          string
}

func (m *MockExecutor) Commit(ctx context.Context, opts *CommitOptions) error {
	m.CommitCalled = true
	m.CommitMessage = opts.Message
	return m.ReturnError
}

func (m *MockExecutor) CommitAll(ctx context.Context, message string) error {
	m.CommitCalled = true
	m.CommitMessage = message
	return m.ReturnError
}

func (m *MockExecutor) Stage(ctx context.Context, files []string) error {
	m.StagedFiles = files
	return m.ReturnError
}

func (m *MockExecutor) StageAll(ctx context.Context) error {
	return m.ReturnError
}

func (m *MockExecutor) HasChanges(ctx context.Context) (bool, error) {
	return m.ChangesExist, m.ReturnError
}

func (m *MockExecutor) CleanLocks(ctx context.Context) (*RemovalResult, error) {
	return &RemovalResult{}, m.ReturnError
}

func (m *MockExecutor) Path() string {
	return m.path
}

func TestMockExecutor_ImplementsInterface(t *testing.T) {
	mock := &MockExecutor{path: "/tmp/test"}
	var _ GitExecutor = mock
}

func TestNewSubmoduleExecutor_NotSubmodule(t *testing.T) {
	tmpDir := setupGitRepo(t)

	_, err := NewSubmoduleExecutor(tmpDir)
	if err == nil {
		t.Error("NewSubmoduleExecutor() should fail for non-submodule")
	}
}

func TestNewSubmoduleExecutor_Valid(t *testing.T) {
	tmpDir := t.TempDir()

	// Create parent git structure
	parentGitDir := filepath.Join(tmpDir, ".git", "modules", "sub")
	os.MkdirAll(parentGitDir, 0755)

	// Create parent .git directory
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	// Create submodule with gitdir file
	subDir := filepath.Join(tmpDir, "sub")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, ".git"), []byte("gitdir: ../.git/modules/sub"), 0644)

	exec, err := NewSubmoduleExecutor(subDir)
	if err != nil {
		t.Fatalf("NewSubmoduleExecutor() error = %v", err)
	}

	if exec.Info().Path != subDir {
		t.Errorf("Info().Path = %v, want %v", exec.Info().Path, subDir)
	}
	if exec.Info().ParentRepo != tmpDir {
		t.Errorf("Info().ParentRepo = %v, want %v", exec.Info().ParentRepo, tmpDir)
	}
}
