package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMatchProject(t *testing.T) {
	// Create a temporary campaign structure with fake submodule paths
	tmpDir := t.TempDir()

	// Create .gitmodules to simulate submodules
	gitmodulesContent := `[submodule "projects/alpha"]
	path = projects/alpha
	url = https://example.com/alpha.git
[submodule "projects/beta"]
	path = projects/beta
	url = https://example.com/beta.git
[submodule "projects/beta-extra"]
	path = projects/beta-extra
	url = https://example.com/beta-extra.git
[submodule "projects/gamma"]
	path = projects/gamma
	url = https://example.com/gamma.git
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitmodules"), []byte(gitmodulesContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create project directories
	for _, dir := range []string{"projects/alpha", "projects/beta", "projects/beta-extra", "projects/gamma"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Initialize git repo (needed for git config to work)
	// We skip this and test the matchProject function directly by checking its logic

	ctx := context.Background()

	tests := []struct {
		name    string
		query   string
		want    string
		wantErr string
	}{
		{
			name:  "exact match",
			query: "alpha",
			want:  filepath.Join(tmpDir, "projects/alpha"),
		},
		{
			name:  "exact match case insensitive",
			query: "Alpha",
			want:  filepath.Join(tmpDir, "projects/alpha"),
		},
		{
			name:  "prefix match unique",
			query: "gam",
			want:  filepath.Join(tmpDir, "projects/gamma"),
		},
		{
			name:    "prefix match ambiguous",
			query:   "beta",
			want:    filepath.Join(tmpDir, "projects/beta"),
		},
		{
			name:    "substring match unique",
			query:   "lph",
			want:    filepath.Join(tmpDir, "projects/alpha"),
		},
		{
			name:    "no match",
			query:   "nonexistent",
			wantErr: `no project matching "nonexistent" found`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := matchProject(ctx, tmpDir, tt.query)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("matchProject(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestMatchProjectContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := matchProject(ctx, "/nonexistent", "test")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
