package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestList_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create projects directory but no projects
	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(projects) != 0 {
		t.Errorf("List() returned %d projects, want 0", len(projects))
	}
}

func TestList_NoProjectsDir(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Don't create projects directory

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if projects != nil {
		t.Errorf("List() = %v, want nil", projects)
	}
}

func TestList_WithProjects(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	// Create a Go project
	goProject := filepath.Join(projectsDir, "go-project")
	os.MkdirAll(goProject, 0755)
	initGitRepo(t, goProject)
	os.WriteFile(filepath.Join(goProject, "go.mod"), []byte("module test"), 0644)

	// Create a Rust project
	rustProject := filepath.Join(projectsDir, "rust-project")
	os.MkdirAll(rustProject, 0755)
	initGitRepo(t, rustProject)
	os.WriteFile(filepath.Join(rustProject, "Cargo.toml"), []byte("[package]"), 0644)

	// Create a TypeScript project
	tsProject := filepath.Join(projectsDir, "ts-project")
	os.MkdirAll(tsProject, 0755)
	initGitRepo(t, tsProject)
	os.WriteFile(filepath.Join(tsProject, "package.json"), []byte("{}"), 0644)

	// Create a non-git directory (should be ignored)
	nonGit := filepath.Join(projectsDir, "not-a-project")
	os.MkdirAll(nonGit, 0755)

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(projects) != 3 {
		t.Errorf("List() returned %d projects, want 3", len(projects))
	}

	// Build map for easier checking
	projectMap := make(map[string]Project)
	for _, p := range projects {
		projectMap[p.Name] = p
	}

	// Check Go project
	if p, ok := projectMap["go-project"]; !ok {
		t.Error("missing go-project")
	} else if p.Type != TypeGo {
		t.Errorf("go-project type = %q, want %q", p.Type, TypeGo)
	}

	// Check Rust project
	if p, ok := projectMap["rust-project"]; !ok {
		t.Error("missing rust-project")
	} else if p.Type != TypeRust {
		t.Errorf("rust-project type = %q, want %q", p.Type, TypeRust)
	}

	// Check TypeScript project
	if p, ok := projectMap["ts-project"]; !ok {
		t.Error("missing ts-project")
	} else if p.Type != TypeTypeScript {
		t.Errorf("ts-project type = %q, want %q", p.Type, TypeTypeScript)
	}
}

func TestList_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := List(ctx, "/some/path")
	if err != context.Canceled {
		t.Errorf("List() error = %v, want %v", err, context.Canceled)
	}
}

func TestList_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := List(ctx, "/some/path")
	if err != context.DeadlineExceeded {
		t.Errorf("List() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestDetectProjectType(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	tests := []struct {
		name     string
		file     string
		content  string
		expected string
	}{
		{"Go", "go.mod", "module test", TypeGo},
		{"Rust", "Cargo.toml", "[package]", TypeRust},
		{"TypeScript", "package.json", "{}", TypeTypeScript},
		{"Python pyproject", "pyproject.toml", "[project]", TypePython},
		{"Python setup", "setup.py", "", TypePython},
		{"Python requirements", "requirements.txt", "", TypePython},
		{"Unknown", "", "", TypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(tmpDir, tt.name)
			os.MkdirAll(dir, 0755)

			if tt.file != "" {
				os.WriteFile(filepath.Join(dir, tt.file), []byte(tt.content), 0644)
			}

			got := detectProjectType(dir)
			if got != tt.expected {
				t.Errorf("detectProjectType() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestList_SkipsFiles(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	// Create a regular file (should be skipped)
	os.WriteFile(filepath.Join(projectsDir, "README.md"), []byte("# Projects"), 0644)

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(projects) != 0 {
		t.Errorf("List() returned %d projects, want 0", len(projects))
	}
}

func TestList_GitSubmodule(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	// Create a directory with a .git file (submodule)
	submodule := filepath.Join(projectsDir, "submodule")
	os.MkdirAll(submodule, 0755)
	// Submodules have a .git file pointing to the parent's .git/modules
	os.WriteFile(filepath.Join(submodule, ".git"), []byte("gitdir: ../../.git/modules/submodule"), 0644)

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(projects) != 1 {
		t.Errorf("List() returned %d projects, want 1", len(projects))
	}
}

// Helper to initialize a git repo
func initGitRepo(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("git", "init", path)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
}
