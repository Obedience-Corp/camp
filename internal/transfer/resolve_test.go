package transfer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSpec(t *testing.T) {
	tests := []struct {
		name     string
		spec     string
		prefix   string
		relPath  string
		hasColon bool
	}{
		{"campaign:path", "other-camp:docs/file.md", "other-camp", "docs/file.md", true},
		{"campaign only", "other-camp:", "other-camp", "", true},
		{"plain path", "docs/file.md", "docs/file.md", "", false},
		{"no colon", "/absolute/path", "/absolute/path", "", false},
		{"campaign:nested", "my-camp:festivals/planned/fest.md", "my-camp", "festivals/planned/fest.md", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, relPath, hasColon := parseSpec(tt.spec)
			if prefix != tt.prefix {
				t.Errorf("prefix = %q, want %q", prefix, tt.prefix)
			}
			if relPath != tt.relPath {
				t.Errorf("relPath = %q, want %q", relPath, tt.relPath)
			}
			if hasColon != tt.hasColon {
				t.Errorf("hasColon = %v, want %v", hasColon, tt.hasColon)
			}
		})
	}
}

func TestResolveCampaignRelative(t *testing.T) {
	tests := []struct {
		name     string
		campRoot string
		path     string
		want     string
	}{
		{
			name:     "relative path",
			campRoot: "/home/user/campaign",
			path:     "workflow/design/active",
			want:     "/home/user/campaign/workflow/design/active",
		},
		{
			name:     "absolute path",
			campRoot: "/home/user/campaign",
			path:     "/other/absolute/path",
			want:     "/other/absolute/path",
		},
		{
			name:     "nested relative",
			campRoot: "/camp",
			path:     "festivals/active/my-fest/OVERVIEW.md",
			want:     "/camp/festivals/active/my-fest/OVERVIEW.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveCampaignRelative(tt.campRoot, tt.path)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveCrossCampaignPathPlain(t *testing.T) {
	ctx := context.Background()
	root := "/fake/campaign/root"

	result, err := ResolveCrossCampaignPath(ctx, root, "docs/test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, "docs/test.md")
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}
}

func TestResolveCrossCampaignPathCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ResolveCrossCampaignPath(ctx, "/fake/root", "other-camp:docs/file.md")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestValidatePathExists(t *testing.T) {
	tmpDir := t.TempDir()
	existing := filepath.Join(tmpDir, "exists.txt")
	if err := os.WriteFile(existing, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ValidatePathExists(existing); err != nil {
		t.Errorf("existing path should not error: %v", err)
	}
	if err := ValidatePathExists(filepath.Join(tmpDir, "nope.txt")); err == nil {
		t.Error("missing path should error")
	}
}

func TestIsDestDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a directory and a file
	dirPath := filepath.Join(tmpDir, "mydir")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(tmpDir, "myfile.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"trailing slash", "some/path/", true},
		{"existing directory", dirPath, true},
		{"existing file", filePath, false},
		{"nonexistent", filepath.Join(tmpDir, "nope"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDestDir(tt.path); got != tt.want {
				t.Errorf("IsDestDir(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
