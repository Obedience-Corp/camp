package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveTarget_Default(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create a git repo
	initGitRepo(t, tmpDir)

	result, err := ResolveTarget(ctx, tmpDir, false, "")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}

	if result.Path != tmpDir {
		t.Errorf("Path = %q, want %q", result.Path, tmpDir)
	}
	if result.IsSubmodule {
		t.Error("IsSubmodule should be false for campaign root")
	}
	if result.Name != "campaign root" {
		t.Errorf("Name = %q, want %q", result.Name, "campaign root")
	}
}

func TestResolveTarget_ExplicitProject(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create campaign root repo
	initGitRepo(t, tmpDir)

	// Create a project directory with its own git repo
	projectDir := filepath.Join(tmpDir, "projects", "myproject")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, projectDir)

	result, err := ResolveTarget(ctx, tmpDir, false, "projects/myproject")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}

	if result.Path != projectDir {
		t.Errorf("Path = %q, want %q", result.Path, projectDir)
	}
	if result.Name != "myproject" {
		t.Errorf("Name = %q, want %q", result.Name, "myproject")
	}
}

func TestResolveTarget_InvalidProject(t *testing.T) {
	ctx := context.Background()
	// Use a fresh temp dir with no git repo anywhere in its hierarchy
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Create a directory that is NOT a git repo and is isolated
	isolatedDir := filepath.Join(tmpDir, "isolated")
	if err := os.MkdirAll(isolatedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Point to a path that doesn't exist at all
	_, err := ResolveTarget(ctx, isolatedDir, false, "/nonexistent/absolute/path/that/does/not/exist")
	if err == nil {
		t.Error("ResolveTarget() should error for nonexistent absolute project path")
	}
}

func TestResolveTarget_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ResolveTarget(ctx, "/tmp/fake", false, "")
	if err == nil {
		t.Error("ResolveTarget() should error when context is cancelled")
	}
}

func TestExtractSubFlags_NoFlags(t *testing.T) {
	args := []string{"--oneline", "-5"}
	remaining, sub, project := ExtractSubFlags(args)

	if sub {
		t.Error("sub should be false")
	}
	if project != "" {
		t.Errorf("project = %q, want empty", project)
	}
	if len(remaining) != 2 || remaining[0] != "--oneline" || remaining[1] != "-5" {
		t.Errorf("remaining = %v, want [--oneline -5]", remaining)
	}
}

func TestExtractSubFlags_SubFlag(t *testing.T) {
	args := []string{"--sub", "--oneline"}
	remaining, sub, project := ExtractSubFlags(args)

	if !sub {
		t.Error("sub should be true")
	}
	if len(remaining) != 1 || remaining[0] != "--oneline" {
		t.Errorf("remaining = %v, want [--oneline]", remaining)
	}
	if project != "" {
		t.Errorf("project = %q, want empty", project)
	}
}

func TestExtractSubFlags_ProjectFlag(t *testing.T) {
	args := []string{"-p", "projects/camp", "--oneline"}
	remaining, sub, project := ExtractSubFlags(args)

	if sub {
		t.Error("sub should be false")
	}
	if project != "projects/camp" {
		t.Errorf("project = %q, want %q", project, "projects/camp")
	}
	if len(remaining) != 1 || remaining[0] != "--oneline" {
		t.Errorf("remaining = %v, want [--oneline]", remaining)
	}
}

func TestExtractSubFlags_ProjectLongFlag(t *testing.T) {
	args := []string{"--project", "projects/fest", "-s"}
	remaining, sub, project := ExtractSubFlags(args)

	if sub {
		t.Error("sub should be false")
	}
	if project != "projects/fest" {
		t.Errorf("project = %q, want %q", project, "projects/fest")
	}
	if len(remaining) != 1 || remaining[0] != "-s" {
		t.Errorf("remaining = %v, want [-s]", remaining)
	}
}

func TestExtractSubFlags_ProjectEquals(t *testing.T) {
	args := []string{"--project=projects/camp", "--graph"}
	remaining, sub, project := ExtractSubFlags(args)

	if sub {
		t.Error("sub should be false")
	}
	if project != "projects/camp" {
		t.Errorf("project = %q, want %q", project, "projects/camp")
	}
	if len(remaining) != 1 || remaining[0] != "--graph" {
		t.Errorf("remaining = %v, want [--graph]", remaining)
	}
}

func TestExtractSubFlags_BothFlags(t *testing.T) {
	args := []string{"--sub", "-p", "projects/camp"}
	remaining, sub, project := ExtractSubFlags(args)

	if !sub {
		t.Error("sub should be true")
	}
	if project != "projects/camp" {
		t.Errorf("project = %q, want %q", project, "projects/camp")
	}
	if len(remaining) != 0 {
		t.Errorf("remaining = %v, want empty", remaining)
	}
}

func TestExtractSubFlags_ProjectAtEnd(t *testing.T) {
	// -p at end without value should not panic
	args := []string{"-p"}
	remaining, _, project := ExtractSubFlags(args)

	if project != "" {
		t.Errorf("project = %q, want empty (no value provided)", project)
	}
	if len(remaining) != 0 {
		t.Errorf("remaining = %v, want empty", remaining)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", dir)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME="+t.TempDir(),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
}
