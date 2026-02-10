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

func TestList_MonorepoExpansion(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	// Create a monorepo with 3 subprojects
	mono := filepath.Join(projectsDir, "my-monorepo")
	os.MkdirAll(mono, 0755)
	initGitRepo(t, mono)

	// Root go.mod (monorepo root)
	os.WriteFile(filepath.Join(mono, "go.mod"), []byte("module mono"), 0644)

	// Subproject 1: Go
	sub1 := filepath.Join(mono, "service-a")
	os.MkdirAll(sub1, 0755)
	os.WriteFile(filepath.Join(sub1, "go.mod"), []byte("module mono/service-a"), 0644)

	// Subproject 2: Go
	sub2 := filepath.Join(mono, "service-b")
	os.MkdirAll(sub2, 0755)
	os.WriteFile(filepath.Join(sub2, "go.mod"), []byte("module mono/service-b"), 0644)

	// Subproject 3: Rust
	sub3 := filepath.Join(mono, "rust-lib")
	os.MkdirAll(sub3, 0755)
	os.WriteFile(filepath.Join(sub3, "Cargo.toml"), []byte("[package]"), 0644)

	// Non-subproject dir (no marker)
	os.MkdirAll(filepath.Join(mono, "docs"), 0755)

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(projects) != 3 {
		t.Fatalf("List() returned %d projects, want 3", len(projects))
	}

	projectMap := make(map[string]Project)
	for _, p := range projects {
		projectMap[p.Name] = p
	}

	// Verify subprojects
	for _, name := range []string{"my-monorepo/service-a", "my-monorepo/service-b", "my-monorepo/rust-lib"} {
		p, ok := projectMap[name]
		if !ok {
			t.Errorf("missing expected subproject %q", name)
			continue
		}
		if p.MonorepoRoot != "projects/my-monorepo" {
			t.Errorf("%s MonorepoRoot = %q, want %q", name, p.MonorepoRoot, "projects/my-monorepo")
		}
	}

	// Verify types
	if p := projectMap["my-monorepo/service-a"]; p.Type != TypeGo {
		t.Errorf("service-a type = %q, want %q", p.Type, TypeGo)
	}
	if p := projectMap["my-monorepo/rust-lib"]; p.Type != TypeRust {
		t.Errorf("rust-lib type = %q, want %q", p.Type, TypeRust)
	}
}

func TestList_StandaloneNotExpanded(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	// Project with only 1 subdirectory marker — should NOT be expanded
	proj := filepath.Join(projectsDir, "single-sub")
	os.MkdirAll(proj, 0755)
	initGitRepo(t, proj)
	os.WriteFile(filepath.Join(proj, "go.mod"), []byte("module root"), 0644)

	sub := filepath.Join(proj, "cmd")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "go.mod"), []byte("module root/cmd"), 0644)

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(projects) != 1 {
		t.Fatalf("List() returned %d projects, want 1", len(projects))
	}

	if projects[0].Name != "single-sub" {
		t.Errorf("project name = %q, want %q", projects[0].Name, "single-sub")
	}
	if projects[0].MonorepoRoot != "" {
		t.Errorf("MonorepoRoot = %q, want empty (standalone)", projects[0].MonorepoRoot)
	}
}

func TestList_MonorepoSkipsSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	// Create a monorepo
	mono := filepath.Join(projectsDir, "mono")
	os.MkdirAll(mono, 0755)
	initGitRepo(t, mono)

	// Two real subprojects
	for _, name := range []string{"svc-a", "svc-b"} {
		sub := filepath.Join(mono, name)
		os.MkdirAll(sub, 0755)
		os.WriteFile(filepath.Join(sub, "go.mod"), []byte("module mono/"+name), 0644)
	}

	// Symlink subdir (should be skipped)
	externalDir := t.TempDir()
	os.WriteFile(filepath.Join(externalDir, "go.mod"), []byte("module external"), 0644)
	os.Symlink(externalDir, filepath.Join(mono, "linked-sub"))

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(projects) != 2 {
		t.Fatalf("List() returned %d projects, want 2 (symlink excluded)", len(projects))
	}

	for _, p := range projects {
		if strings.Contains(p.Name, "linked-sub") {
			t.Errorf("symlinked subdirectory should not appear as subproject: %s", p.Name)
		}
	}
}

func TestList_MonorepoSkipsExcludedDirs(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	mono := filepath.Join(projectsDir, "mono")
	os.MkdirAll(mono, 0755)
	initGitRepo(t, mono)

	// Two real subprojects
	for _, name := range []string{"app", "lib"} {
		sub := filepath.Join(mono, name)
		os.MkdirAll(sub, 0755)
		os.WriteFile(filepath.Join(sub, "package.json"), []byte("{}"), 0644)
	}

	// vendor/ and node_modules/ with markers (should be excluded)
	for _, excluded := range []string{"vendor", "node_modules", "testdata"} {
		dir := filepath.Join(mono, excluded)
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644)
	}

	// Hidden dir with marker (should be excluded)
	hidden := filepath.Join(mono, ".internal")
	os.MkdirAll(hidden, 0755)
	os.WriteFile(filepath.Join(hidden, "go.mod"), []byte("module hidden"), 0644)

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(projects) != 2 {
		t.Fatalf("List() returned %d projects, want 2 (excluded dirs filtered)", len(projects))
	}
}

func TestList_DeduplicatesByRemoteURL(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	remoteURL := "git@github.com:Obedience-Corp/camp.git"

	// Create first project with a commit
	proj1 := filepath.Join(projectsDir, "camp")
	os.MkdirAll(proj1, 0755)
	initGitRepoWithRemoteAndCommit(t, proj1, remoteURL, "first commit")
	os.WriteFile(filepath.Join(proj1, "go.mod"), []byte("module camp"), 0644)

	// Create second project with the same remote (newer commit)
	proj2 := filepath.Join(projectsDir, "camp-copy")
	os.MkdirAll(proj2, 0755)
	initGitRepoWithRemoteAndCommit(t, proj2, remoteURL, "second commit")
	os.WriteFile(filepath.Join(proj2, "go.mod"), []byte("module camp"), 0644)

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(projects) != 1 {
		names := make([]string, len(projects))
		for i, p := range projects {
			names[i] = p.Name
		}
		t.Fatalf("List() returned %d projects %v, want 1 (deduped by URL)", len(projects), names)
	}
}

func TestList_NoDedupeWithoutURL(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	// Create two projects with no remote (empty URL)
	for _, name := range []string{"local-a", "local-b"} {
		proj := filepath.Join(projectsDir, name)
		os.MkdirAll(proj, 0755)
		initGitRepo(t, proj)
		os.WriteFile(filepath.Join(proj, "go.mod"), []byte("module "+name), 0644)
	}

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(projects) != 2 {
		t.Fatalf("List() returned %d projects, want 2 (no dedup for empty URL)", len(projects))
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

// initGitRepoWithRemoteAndCommit initializes a git repo, sets a remote URL,
// and creates an initial commit so that git log returns a date.
func initGitRepoWithRemoteAndCommit(t *testing.T, path, remoteURL, message string) {
	t.Helper()

	cmds := [][]string{
		{"git", "init", path},
		{"git", "-C", path, "remote", "add", "origin", remoteURL},
		{"git", "-C", path, "config", "user.email", "test@test.com"},
		{"git", "-C", path, "config", "user.name", "Test"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if err := cmd.Run(); err != nil {
			t.Fatalf("command %v failed: %v", args, err)
		}
	}

	// Create a file and commit it
	os.WriteFile(filepath.Join(path, "README.md"), []byte(message), 0644)
	cmd := exec.Command("git", "-C", path, "add", ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	cmd = exec.Command("git", "-C", path, "commit", "-m", message)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}
}
