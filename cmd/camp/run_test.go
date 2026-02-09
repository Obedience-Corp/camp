package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsProject(t *testing.T) {
	tmpDir := t.TempDir()
	projectsDir := filepath.Join(tmpDir, "projects")
	require.NoError(t, os.MkdirAll(projectsDir, 0o755))

	// Create a valid project (has .git)
	validProject := filepath.Join(projectsDir, "my-project")
	require.NoError(t, os.MkdirAll(filepath.Join(validProject, ".git"), 0o755))

	// Create a non-project directory (no .git)
	require.NoError(t, os.MkdirAll(filepath.Join(projectsDir, "not-a-project"), 0o755))

	tests := []struct {
		name     string
		query    string
		wantOk   bool
		wantPath string
	}{
		{
			name:     "valid project with .git",
			query:    "my-project",
			wantOk:   true,
			wantPath: validProject,
		},
		{
			name:   "directory without .git",
			query:  "not-a-project",
			wantOk: false,
		},
		{
			name:   "nonexistent directory",
			query:  "does-not-exist",
			wantOk: false,
		},
		{
			name:   "empty string",
			query:  "",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ok := isProject(tmpDir, tt.query)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.wantPath, path)
			}
		})
	}
}
