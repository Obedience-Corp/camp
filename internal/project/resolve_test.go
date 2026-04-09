package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupTestCampaign creates a temporary campaign root with a projects directory
// and one or more fake git repos inside it.
func setupTestCampaign(t *testing.T, projectNames ...string) string {
	t.Helper()

	campRoot := t.TempDir()
	projectsDir := filepath.Join(campRoot, "projects")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, name := range projectNames {
		projDir := filepath.Join(projectsDir, name)
		if err := os.MkdirAll(projDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Initialize as a git repo so List() picks it up
		cmd := exec.Command("git", "init", projDir)
		if err := cmd.Run(); err != nil {
			t.Fatalf("git init %s: %v", name, err)
		}
		// Create an initial commit so rev-parse works
		cmd = exec.Command("git", "-C", projDir, "commit", "--allow-empty", "-m", "init")
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if err := cmd.Run(); err != nil {
			t.Fatalf("git commit %s: %v", name, err)
		}
	}

	return campRoot
}

func TestResolveByName(t *testing.T) {
	campRoot := setupTestCampaign(t, "alpha", "beta", "gamma")

	tests := []struct {
		name    string
		lookup  string
		wantErr bool
	}{
		{
			name:   "existing project",
			lookup: "alpha",
		},
		{
			name:   "another existing project",
			lookup: "beta",
		},
		{
			name:    "nonexistent project",
			lookup:  "nope",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			absPath, err := ResolveByName(ctx, campRoot, tt.lookup)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				// Should be a ProjectNotFoundError
				if _, ok := err.(*ProjectNotFoundError); !ok {
					t.Fatalf("expected *ProjectNotFoundError, got %T: %v", err, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expected := filepath.Join(campRoot, "projects", tt.lookup)
			if absPath != expected {
				t.Fatalf("got path %q, want %q", absPath, expected)
			}
		})
	}
}

func TestResolveByName_ContextCancelled(t *testing.T) {
	campRoot := setupTestCampaign(t, "alpha")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ResolveByName(ctx, campRoot, "alpha")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestResolveFromCwd(t *testing.T) {
	campRoot := setupTestCampaign(t, "myproj")

	// Save and restore cwd
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	projDir := filepath.Join(campRoot, "projects", "myproj")
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	result, err := ResolveFromCwd(ctx, campRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Name != "myproj" {
		t.Fatalf("got name %q, want %q", result.Name, "myproj")
	}

	// Resolve the expected path through EvalSymlinks for macOS /var → /private/var
	expectedPath, _ := filepath.EvalSymlinks(projDir)
	gotPath, _ := filepath.EvalSymlinks(result.Path)
	if gotPath != expectedPath {
		t.Fatalf("got path %q, want %q", gotPath, expectedPath)
	}
}

func TestResolve_WithFlag(t *testing.T) {
	campRoot := setupTestCampaign(t, "api", "web")

	ctx := context.Background()
	result, err := Resolve(ctx, campRoot, "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Name != "web" {
		t.Fatalf("got name %q, want %q", result.Name, "web")
	}

	expected := filepath.Join(campRoot, "projects", "web")
	if result.Path != expected {
		t.Fatalf("got path %q, want %q", result.Path, expected)
	}
	if result.LogicalPath != filepath.Join("projects", "web") {
		t.Fatalf("got logical path %q, want %q", result.LogicalPath, filepath.Join("projects", "web"))
	}
	if result.Source != SourceSubmodule {
		t.Fatalf("got source %q, want %q", result.Source, SourceSubmodule)
	}
}

func TestResolve_WithFlag_NotFound(t *testing.T) {
	campRoot := setupTestCampaign(t, "api")

	ctx := context.Background()
	_, err := Resolve(ctx, campRoot, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}

	var notFound *ProjectNotFoundError
	if ok := isProjectNotFound(err); !ok {
		t.Fatalf("expected ProjectNotFoundError, got %T: %v", err, err)
	}
	_ = notFound
}

func isProjectNotFound(err error) bool {
	_, ok := err.(*ProjectNotFoundError)
	return ok
}

func TestFormatProjectList(t *testing.T) {
	projects := []Project{
		{Name: "alpha", Path: "projects/alpha"},
		{Name: "beta", Path: "projects/beta"},
	}

	result := FormatProjectList(projects)
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// Should mention both projects
	if !containsString(result, "alpha") || !containsString(result, "beta") {
		t.Fatalf("expected project names in output, got: %s", result)
	}
}

func TestFormatProjectList_Empty(t *testing.T) {
	result := FormatProjectList(nil)
	if result == "" {
		t.Fatal("expected non-empty result for empty list")
	}
	if !containsString(result, "No projects") {
		t.Fatalf("expected 'No projects' message, got: %s", result)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
