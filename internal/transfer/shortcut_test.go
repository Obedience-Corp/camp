package transfer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func setupCampaign(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	gitmodulesContent := `[submodule "projects/alpha"]
	path = projects/alpha
	url = https://example.com/alpha.git
[submodule "projects/beta"]
	path = projects/beta
	url = https://example.com/beta.git
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitmodules"), []byte(gitmodulesContent), 0644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{
		"projects/alpha/internal",
		"projects/alpha/cmd",
		"projects/beta/internal",
		"projects/beta/cmd",
	} {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Create a source file
	if err := os.WriteFile(filepath.Join(tmpDir, "projects/alpha/internal/foo.go"), []byte("package internal"), 0644); err != nil {
		t.Fatal(err)
	}
	return tmpDir
}

func TestResolveShortcut(t *testing.T) {
	campRoot := setupCampaign(t)
	ctx := context.Background()

	tests := []struct {
		name          string
		cwd           string
		file          string
		targetProject string
		wantSrc       string
		wantDest      string
		wantErr       bool
	}{
		{
			name:          "valid shortcut",
			cwd:           filepath.Join(campRoot, "projects/alpha"),
			file:          "internal/foo.go",
			targetProject: "beta",
			wantSrc:       filepath.Join(campRoot, "projects/alpha/internal/foo.go"),
			wantDest:      filepath.Join(campRoot, "projects/beta/internal/foo.go"),
		},
		{
			name:          "file outside project",
			cwd:           filepath.Join(campRoot, "projects/alpha"),
			file:          "../../README.md",
			targetProject: "beta",
			wantErr:       true,
		},
		{
			name:          "unknown target project",
			cwd:           filepath.Join(campRoot, "projects/alpha"),
			file:          "internal/foo.go",
			targetProject: "nonexistent",
			wantErr:       true,
		},
		{
			name:          "nested path",
			cwd:           filepath.Join(campRoot, "projects/alpha/internal"),
			file:          "foo.go",
			targetProject: "beta",
			wantSrc:       filepath.Join(campRoot, "projects/alpha/internal/foo.go"),
			wantDest:      filepath.Join(campRoot, "projects/beta/internal/foo.go"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dest, err := ResolveShortcut(ctx, campRoot, tt.cwd, tt.file, tt.targetProject)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if src != tt.wantSrc {
				t.Errorf("src = %q, want %q", src, tt.wantSrc)
			}
			if dest != tt.wantDest {
				t.Errorf("dest = %q, want %q", dest, tt.wantDest)
			}
		})
	}
}

func TestResolveShortcutCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := ResolveShortcut(ctx, "/fake", "/fake/projects/alpha", "file.go", "beta")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestDetectCurrentProject(t *testing.T) {
	campRoot := setupCampaign(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		dir      string
		wantName string
		wantErr  bool
	}{
		{
			name:     "project root",
			dir:      filepath.Join(campRoot, "projects/alpha"),
			wantName: "alpha",
		},
		{
			name:     "nested in project",
			dir:      filepath.Join(campRoot, "projects/alpha/internal"),
			wantName: "alpha",
		},
		{
			name:    "outside project",
			dir:     campRoot,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, _, err := detectCurrentProject(ctx, campRoot, tt.dir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}
