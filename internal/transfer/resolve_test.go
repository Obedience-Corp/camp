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
		project  string
		relPath  string
		hasColon bool
	}{
		{"project:path", "fest:cmd/main.go", "fest", "cmd/main.go", true},
		{"project only", "fest:", "fest", "", true},
		{"plain path", "cmd/main.go", "cmd/main.go", "", false},
		{"no colon", "/absolute/path", "/absolute/path", "", false},
		{"project:nested", "camp:internal/pins/pins.go", "camp", "internal/pins/pins.go", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project, relPath, hasColon := parseSpec(tt.spec)
			if project != tt.project {
				t.Errorf("project = %q, want %q", project, tt.project)
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

func TestResolveCampaignPathPlain(t *testing.T) {
	ctx := context.Background()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	result, err := ResolveCampaignPath(ctx, "/fake/root", "relative/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(cwd, "relative/file.txt")
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}
}

func TestResolveCampaignPathCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ResolveCampaignPath(ctx, "/fake/root", "fest:cmd/main.go")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestResolveCampaignPathProject(t *testing.T) {
	// Create a temporary campaign structure
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
	for _, dir := range []string{"projects/alpha", "projects/beta"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()

	tests := []struct {
		name    string
		spec    string
		want    string
		wantErr bool
	}{
		{
			name: "exact project",
			spec: "alpha:README.md",
			want: filepath.Join(tmpDir, "projects/alpha", "README.md"),
		},
		{
			name:    "missing project",
			spec:    "nonexistent:file.txt",
			wantErr: true,
		},
		{
			name: "project with nested path",
			spec: "beta:cmd/main.go",
			want: filepath.Join(tmpDir, "projects/beta", "cmd/main.go"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveCampaignPath(ctx, tmpDir, tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidatePathExists(t *testing.T) {
	tmpDir := t.TempDir()
	existing := filepath.Join(tmpDir, "exists.txt")
	if err := os.WriteFile(existing, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ValidatePathExists(existing); err != nil {
		t.Errorf("existing path should not error: %v", err)
	}
	if err := ValidatePathExists(filepath.Join(tmpDir, "nope.txt")); err == nil {
		t.Error("missing path should error")
	}
}
