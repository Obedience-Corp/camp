package project

import (
	"context"
	"fmt"
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

func TestList_GitmodulesExpansion(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	// Create a repo with .gitmodules listing 2 submodules
	mono := filepath.Join(projectsDir, "my-monorepo")
	os.MkdirAll(mono, 0755)
	initGitRepo(t, mono)

	// Root go.mod
	os.WriteFile(filepath.Join(mono, "go.mod"), []byte("module mono"), 0644)

	// .gitmodules declares two submodules
	writeGitmodules(t, mono, map[string]string{
		"service-a": "service-a",
		"service-b": "service-b",
	})

	// Create submodule directories with language markers
	sub1 := filepath.Join(mono, "service-a")
	os.MkdirAll(sub1, 0755)
	os.WriteFile(filepath.Join(sub1, "go.mod"), []byte("module mono/service-a"), 0644)

	sub2 := filepath.Join(mono, "service-b")
	os.MkdirAll(sub2, 0755)
	os.WriteFile(filepath.Join(sub2, "Cargo.toml"), []byte("[package]"), 0644)

	// Non-submodule dir with a language marker (should NOT become a subproject)
	lib := filepath.Join(mono, "lib")
	os.MkdirAll(lib, 0755)
	os.WriteFile(filepath.Join(lib, "go.mod"), []byte("module mono/lib"), 0644)

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Expect: root entry + 2 submodule entries = 3 (NOT lib, it's not in .gitmodules)
	if len(projects) != 3 {
		names := make([]string, len(projects))
		for i, p := range projects {
			names[i] = p.Name
		}
		t.Fatalf("List() returned %d projects %v, want 3 (root + 2 submodules)", len(projects), names)
	}

	projectMap := make(map[string]Project)
	for _, p := range projects {
		projectMap[p.Name] = p
	}

	// Verify root entry exists
	root, ok := projectMap["my-monorepo"]
	if !ok {
		t.Fatal("missing root entry 'my-monorepo'")
	}
	if root.MonorepoRoot != "" {
		t.Errorf("root MonorepoRoot = %q, want empty", root.MonorepoRoot)
	}
	if root.Type != TypeGo {
		t.Errorf("root type = %q, want %q", root.Type, TypeGo)
	}
	// Root entry should carry ExcludeDirs for scc double-count prevention
	if len(root.ExcludeDirs) != 2 {
		t.Errorf("root ExcludeDirs = %v, want 2 entries", root.ExcludeDirs)
	}

	// Verify submodule entries
	for _, name := range []string{"my-monorepo@service-a", "my-monorepo@service-b"} {
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
	if p := projectMap["my-monorepo@service-a"]; p.Type != TypeGo {
		t.Errorf("service-a type = %q, want %q", p.Type, TypeGo)
	}
	if p := projectMap["my-monorepo@service-b"]; p.Type != TypeRust {
		t.Errorf("service-b type = %q, want %q", p.Type, TypeRust)
	}

	// Verify lib is NOT present (it's not in .gitmodules)
	if _, ok := projectMap["my-monorepo@lib"]; ok {
		t.Error("lib should not be a subproject — it's not in .gitmodules")
	}
}

func TestList_NoGitmodulesStandalone(t *testing.T) {
	// Regression test: repos without .gitmodules should NEVER be expanded,
	// even if they have multiple subdirectories with language markers.
	// This is the hermes bug — language markers caused false monorepo detection.
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	// Create a repo that looks like hermes: multiple language-marker subdirs but NO .gitmodules
	hermes := filepath.Join(projectsDir, "hermes")
	os.MkdirAll(hermes, 0755)
	initGitRepo(t, hermes)

	// Root has no marker
	// common/ and loadtests/ both have package.json
	common := filepath.Join(hermes, "common")
	os.MkdirAll(common, 0755)
	os.WriteFile(filepath.Join(common, "package.json"), []byte("{}"), 0644)

	loadtests := filepath.Join(hermes, "loadtests")
	os.MkdirAll(loadtests, 0755)
	os.WriteFile(filepath.Join(loadtests, "package.json"), []byte("{}"), 0644)

	// services/ has no marker (but has real code)
	os.MkdirAll(filepath.Join(hermes, "services"), 0755)

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Should be 1 standalone entry, NOT expanded
	if len(projects) != 1 {
		names := make([]string, len(projects))
		for i, p := range projects {
			names[i] = p.Name
		}
		t.Fatalf("List() returned %d projects %v, want 1 standalone entry (no .gitmodules = no expansion)", len(projects), names)
	}

	if projects[0].Name != "hermes" {
		t.Errorf("project name = %q, want %q", projects[0].Name, "hermes")
	}
	if projects[0].MonorepoRoot != "" {
		t.Errorf("MonorepoRoot = %q, want empty (standalone)", projects[0].MonorepoRoot)
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

func TestList_GitmodulesSubmoduleDedupAgainstStandalone(t *testing.T) {
	// When a .gitmodules repo has a submodule named "foo" and there's also
	// a standalone project named "foo", the submodule entry should be deduped.
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	projectsDir := filepath.Join(tmpDir, "projects")
	os.MkdirAll(projectsDir, 0755)

	remoteURL := "git@github.com:test/foo.git"

	// Standalone "foo" project
	foo := filepath.Join(projectsDir, "foo")
	os.MkdirAll(foo, 0755)
	initGitRepoWithRemoteAndCommit(t, foo, remoteURL, "standalone foo")
	os.WriteFile(filepath.Join(foo, "go.mod"), []byte("module foo"), 0644)

	// Monorepo with .gitmodules listing "foo" as a submodule
	mono := filepath.Join(projectsDir, "mono")
	os.MkdirAll(mono, 0755)
	initGitRepoWithRemoteAndCommit(t, mono, "git@github.com:test/mono.git", "mono init")

	writeGitmodules(t, mono, map[string]string{
		"foo": "foo",
		"bar": "bar",
	})

	// Create submodule directories
	os.MkdirAll(filepath.Join(mono, "foo"), 0755)
	os.WriteFile(filepath.Join(mono, "foo", "go.mod"), []byte("module mono/foo"), 0644)
	os.MkdirAll(filepath.Join(mono, "bar"), 0755)
	os.WriteFile(filepath.Join(mono, "bar", "go.mod"), []byte("module mono/bar"), 0644)

	ctx := context.Background()
	projects, err := List(ctx, tmpDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	projectMap := make(map[string]Project)
	for _, p := range projects {
		projectMap[p.Name] = p
	}

	// "foo" standalone should exist
	if _, ok := projectMap["foo"]; !ok {
		t.Error("missing standalone 'foo' project")
	}

	// "mono@foo" should be deduped (standalone "foo" takes precedence)
	if _, ok := projectMap["mono@foo"]; ok {
		t.Error("mono@foo should be deduped against standalone 'foo'")
	}

	// "mono@bar" should exist (no standalone "bar")
	if _, ok := projectMap["mono@bar"]; !ok {
		t.Error("missing mono@bar subproject")
	}

	// "mono" root entry should exist
	if _, ok := projectMap["mono"]; !ok {
		t.Error("missing mono root entry")
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

// writeGitmodules writes a .gitmodules file to the given repo path.
// submodules maps submodule name to relative path.
func writeGitmodules(t *testing.T, repoPath string, submodules map[string]string) {
	t.Helper()
	var content string
	for name, path := range submodules {
		content += fmt.Sprintf("[submodule %q]\n\tpath = %s\n\turl = https://example.com/%s.git\n", name, path, name)
	}
	if err := os.WriteFile(filepath.Join(repoPath, ".gitmodules"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write .gitmodules: %v", err)
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
