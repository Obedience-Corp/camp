package worktree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/paths"
)

func newTestPathManager(t *testing.T, root string) *PathManager {
	t.Helper()
	resolver := paths.NewResolver(root, config.DefaultCampaignPaths())
	return NewPathManager(resolver)
}

func TestPathManager_WorktreePath(t *testing.T) {
	pm := newTestPathManager(t, "/campaign")

	path := pm.WorktreePath("my-api", "feature")
	expected := "/campaign/projects/worktrees/my-api/feature"
	if path != expected {
		t.Errorf("WorktreePath() = %q, want %q", path, expected)
	}
}

func TestPathManager_ProjectWorktreesDir(t *testing.T) {
	pm := newTestPathManager(t, "/campaign")

	path := pm.ProjectWorktreesDir("my-api")
	expected := "/campaign/projects/worktrees/my-api"
	if path != expected {
		t.Errorf("ProjectWorktreesDir() = %q, want %q", path, expected)
	}
}

func TestPathManager_WorktreesRoot(t *testing.T) {
	pm := newTestPathManager(t, "/campaign")

	path := pm.WorktreesRoot()
	expected := "/campaign/projects/worktrees"
	if path != expected {
		t.Errorf("WorktreesRoot() = %q, want %q", path, expected)
	}
}

func TestPathManager_ParseWorktreePath(t *testing.T) {
	tmpDir := t.TempDir()
	pm := newTestPathManager(t, tmpDir)

	// Create the worktree directory structure
	wtPath := pm.WorktreePath("my-api", "feature")
	srcPath := filepath.Join(wtPath, "src")
	if err := os.MkdirAll(srcPath, 0755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		path        string
		wantProject string
		wantName    string
		wantErr     bool
	}{
		{
			name:        "worktree root",
			path:        wtPath,
			wantProject: "my-api",
			wantName:    "feature",
			wantErr:     false,
		},
		{
			name:        "nested in worktree",
			path:        srcPath,
			wantProject: "my-api",
			wantName:    "feature",
			wantErr:     false,
		},
		{
			name:    "outside worktrees",
			path:    tmpDir,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project, name, err := pm.ParseWorktreePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseWorktreePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if project != tt.wantProject {
					t.Errorf("project = %q, want %q", project, tt.wantProject)
				}
				if name != tt.wantName {
					t.Errorf("name = %q, want %q", name, tt.wantName)
				}
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"feature-auth", true},
		{"my_worktree", true},
		{"PR123", true},
		{"a", true},
		{"A1", true},
		{"feature_v2", true},
		{"feature-v2-test", true},
		{"", false},
		{"has space", false},
		{"has/slash", false},
		{".hidden", false},
		{".git", false},
		{".gitignore", false},
		{"-starts-with-dash", false},
		{"_starts-with-underscore", false},
		{"ends-with-", true}, // This is valid by the regex
		{
			name:  "a-very-long-name-that-exceeds-sixty-four-characters-in-total-length",
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.name)
			if (err == nil) != tt.valid {
				t.Errorf("ValidateName(%q) = %v, want valid=%v", tt.name, err, tt.valid)
			}
		})
	}
}

func TestPathManager_WorktreeExists(t *testing.T) {
	tmpDir := t.TempDir()
	pm := newTestPathManager(t, tmpDir)

	// Create worktree directory
	wtPath := pm.WorktreePath("my-api", "exists")
	if err := os.MkdirAll(wtPath, 0755); err != nil {
		t.Fatal(err)
	}

	if !pm.WorktreeExists("my-api", "exists") {
		t.Error("WorktreeExists() = false for existing worktree")
	}

	if pm.WorktreeExists("my-api", "nonexistent") {
		t.Error("WorktreeExists() = true for non-existent worktree")
	}
}

func TestPathManager_EnsureWorktreesDir(t *testing.T) {
	tmpDir := t.TempDir()
	pm := newTestPathManager(t, tmpDir)

	if err := pm.EnsureWorktreesDir("new-project"); err != nil {
		t.Errorf("EnsureWorktreesDir() error = %v", err)
	}

	expected := pm.ProjectWorktreesDir("new-project")
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Errorf("Directory %s was not created", expected)
	}
}

func TestPathManager_ListProjectWorktrees(t *testing.T) {
	tmpDir := t.TempDir()
	pm := newTestPathManager(t, tmpDir)

	// Create some worktrees
	worktrees := []string{"feature-a", "feature-b", "bugfix-1"}
	for _, wt := range worktrees {
		if err := os.MkdirAll(pm.WorktreePath("my-api", wt), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Create hidden directory (should be ignored)
	if err := os.MkdirAll(pm.WorktreePath("my-api", ".hidden"), 0755); err != nil {
		t.Fatal(err)
	}

	list, err := pm.ListProjectWorktrees("my-api")
	if err != nil {
		t.Fatalf("ListProjectWorktrees() error = %v", err)
	}

	if len(list) != len(worktrees) {
		t.Errorf("ListProjectWorktrees() returned %d items, want %d", len(list), len(worktrees))
	}
}

func TestPathManager_ListAllProjects(t *testing.T) {
	tmpDir := t.TempDir()
	pm := newTestPathManager(t, tmpDir)

	// Create worktrees for multiple projects
	projects := []string{"api", "frontend", "backend"}
	for _, proj := range projects {
		if err := os.MkdirAll(pm.WorktreePath(proj, "feature"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	list, err := pm.ListAllProjects()
	if err != nil {
		t.Fatalf("ListAllProjects() error = %v", err)
	}

	if len(list) != len(projects) {
		t.Errorf("ListAllProjects() returned %d items, want %d", len(list), len(projects))
	}
}

func TestPathManager_ListProjectWorktrees_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	pm := newTestPathManager(t, tmpDir)

	list, err := pm.ListProjectWorktrees("nonexistent")
	if err != nil {
		t.Fatalf("ListProjectWorktrees() error = %v", err)
	}
	if list != nil {
		t.Errorf("ListProjectWorktrees() = %v, want nil", list)
	}
}
