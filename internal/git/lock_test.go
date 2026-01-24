package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindIndexLocks(t *testing.T) {
	t.Run("finds main lock", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		os.MkdirAll(gitDir, 0755)

		// Create main lock
		mainLock := filepath.Join(gitDir, "index.lock")
		os.WriteFile(mainLock, []byte{}, 0644)

		ctx := context.Background()
		locks, err := FindIndexLocks(ctx, gitDir)
		if err != nil {
			t.Fatalf("FindIndexLocks() error = %v", err)
		}

		if len(locks) != 1 {
			t.Errorf("FindIndexLocks() found %d locks, want 1", len(locks))
		}
		if locks[0] != mainLock {
			t.Errorf("FindIndexLocks() = %v, want %v", locks[0], mainLock)
		}
	})

	t.Run("finds submodule locks", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		os.MkdirAll(gitDir, 0755)

		// Create main lock
		mainLock := filepath.Join(gitDir, "index.lock")
		os.WriteFile(mainLock, []byte{}, 0644)

		// Create submodule lock
		modulesDir := filepath.Join(gitDir, "modules", "subproject")
		os.MkdirAll(modulesDir, 0755)
		subLock := filepath.Join(modulesDir, "index.lock")
		os.WriteFile(subLock, []byte{}, 0644)

		ctx := context.Background()
		locks, err := FindIndexLocks(ctx, gitDir)
		if err != nil {
			t.Fatalf("FindIndexLocks() error = %v", err)
		}

		if len(locks) != 2 {
			t.Errorf("FindIndexLocks() found %d locks, want 2", len(locks))
		}
	})

	t.Run("finds nested submodule locks", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		os.MkdirAll(gitDir, 0755)

		// Create nested submodule structure
		nestedModulesDir := filepath.Join(gitDir, "modules", "project", "modules", "nested")
		os.MkdirAll(nestedModulesDir, 0755)
		nestedLock := filepath.Join(nestedModulesDir, "index.lock")
		os.WriteFile(nestedLock, []byte{}, 0644)

		ctx := context.Background()
		locks, err := FindIndexLocks(ctx, gitDir)
		if err != nil {
			t.Fatalf("FindIndexLocks() error = %v", err)
		}

		if len(locks) != 1 {
			t.Errorf("FindIndexLocks() found %d locks, want 1", len(locks))
		}
	})

	t.Run("returns empty for no locks", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		os.MkdirAll(gitDir, 0755)

		ctx := context.Background()
		locks, err := FindIndexLocks(ctx, gitDir)
		if err != nil {
			t.Fatalf("FindIndexLocks() error = %v", err)
		}

		if len(locks) != 0 {
			t.Errorf("FindIndexLocks() found %d locks, want 0", len(locks))
		}
	})

	t.Run("returns error for non-existent directory", func(t *testing.T) {
		ctx := context.Background()
		_, err := FindIndexLocks(ctx, "/nonexistent/path")
		if err == nil {
			t.Error("FindIndexLocks() expected error for non-existent path")
		}
	})

	t.Run("returns error for file instead of directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "notadir")
		os.WriteFile(filePath, []byte{}, 0644)

		ctx := context.Background()
		_, err := FindIndexLocks(ctx, filePath)
		if err == nil {
			t.Error("FindIndexLocks() expected error for file path")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		os.MkdirAll(gitDir, 0755)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := FindIndexLocks(ctx, gitDir)
		if err == nil {
			t.Error("FindIndexLocks() expected error for cancelled context")
		}
	})
}

func TestResolveGitDir(t *testing.T) {
	t.Run("regular repository", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		os.MkdirAll(gitDir, 0755)

		got, err := ResolveGitDir(tmpDir)
		if err != nil {
			t.Fatalf("ResolveGitDir() error = %v", err)
		}

		if got != gitDir {
			t.Errorf("ResolveGitDir() = %v, want %v", got, gitDir)
		}
	})

	t.Run("submodule with relative path", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create the target modules directory structure
		targetDir := filepath.Join(tmpDir, "parent", ".git", "modules", "myproject")
		os.MkdirAll(targetDir, 0755)

		// Create submodule directory with .git file
		submoduleDir := filepath.Join(tmpDir, "parent", "myproject")
		os.MkdirAll(submoduleDir, 0755)

		gitFile := filepath.Join(submoduleDir, ".git")
		content := "gitdir: ../.git/modules/myproject"
		os.WriteFile(gitFile, []byte(content), 0644)

		got, err := ResolveGitDir(submoduleDir)
		if err != nil {
			t.Fatalf("ResolveGitDir() error = %v", err)
		}

		if !strings.Contains(got, "modules/myproject") {
			t.Errorf("ResolveGitDir() = %v, want path containing modules/myproject", got)
		}
	})

	t.Run("submodule with absolute path", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create the target modules directory
		targetDir := filepath.Join(tmpDir, ".git", "modules", "myproject")
		os.MkdirAll(targetDir, 0755)

		// Create submodule directory with .git file using absolute path
		submoduleDir := filepath.Join(tmpDir, "myproject")
		os.MkdirAll(submoduleDir, 0755)

		gitFile := filepath.Join(submoduleDir, ".git")
		content := "gitdir: " + targetDir
		os.WriteFile(gitFile, []byte(content), 0644)

		got, err := ResolveGitDir(submoduleDir)
		if err != nil {
			t.Fatalf("ResolveGitDir() error = %v", err)
		}

		if got != targetDir {
			t.Errorf("ResolveGitDir() = %v, want %v", got, targetDir)
		}
	})

	t.Run("invalid .git file format", func(t *testing.T) {
		tmpDir := t.TempDir()

		gitFile := filepath.Join(tmpDir, ".git")
		os.WriteFile(gitFile, []byte("invalid content"), 0644)

		_, err := ResolveGitDir(tmpDir)
		if err == nil {
			t.Error("ResolveGitDir() expected error for invalid format")
		}
	})

	t.Run("missing .git", func(t *testing.T) {
		tmpDir := t.TempDir()

		_, err := ResolveGitDir(tmpDir)
		if err == nil {
			t.Error("ResolveGitDir() expected error for missing .git")
		}
	})
}

func TestFindLocksInRepository(t *testing.T) {
	t.Run("finds locks from repo root", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		os.MkdirAll(gitDir, 0755)

		mainLock := filepath.Join(gitDir, "index.lock")
		os.WriteFile(mainLock, []byte{}, 0644)

		ctx := context.Background()
		locks, err := FindLocksInRepository(ctx, tmpDir)
		if err != nil {
			t.Fatalf("FindLocksInRepository() error = %v", err)
		}

		if len(locks) != 1 {
			t.Errorf("FindLocksInRepository() found %d locks, want 1", len(locks))
		}
	})
}

func TestHasLockFile(t *testing.T) {
	t.Run("returns true when lock exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "index.lock")
		os.WriteFile(lockPath, []byte{}, 0644)

		if !HasLockFile(tmpDir) {
			t.Error("HasLockFile() = false, want true")
		}
	})

	t.Run("returns false when lock does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()

		if HasLockFile(tmpDir) {
			t.Error("HasLockFile() = true, want false")
		}
	})
}

func TestParseGitdirFile(t *testing.T) {
	tests := []struct {
		name     string
		repoRoot string
		content  string
		wantErr  bool
	}{
		{
			name:     "valid relative path",
			repoRoot: "/repo/submodule",
			content:  "gitdir: ../.git/modules/submodule",
			wantErr:  false,
		},
		{
			name:     "valid absolute path",
			repoRoot: "/repo/submodule",
			content:  "gitdir: /repo/.git/modules/submodule",
			wantErr:  false,
		},
		{
			name:     "with trailing newline",
			repoRoot: "/repo/submodule",
			content:  "gitdir: ../.git/modules/submodule\n",
			wantErr:  false,
		},
		{
			name:     "invalid format - missing prefix",
			repoRoot: "/repo/submodule",
			content:  "../.git/modules/submodule",
			wantErr:  true,
		},
		{
			name:     "invalid format - wrong prefix",
			repoRoot: "/repo/submodule",
			content:  "path: ../.git/modules/submodule",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseGitdirFile(tt.repoRoot, tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseGitdirFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
