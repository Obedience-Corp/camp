package leverage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveProjects_ConfigDriven(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name      string
		projects  map[string]ProjectEntry
		wantNames []string
		wantErr   bool
	}{
		{
			name: "full_map_resolves",
			projects: map[string]ProjectEntry{
				"camp": {Path: "projects/camp", Include: true},
				"fest": {Path: "projects/fest", Include: true},
				"chat": {Path: "projects/obey-chat", Include: true},
			},
			wantNames: []string{"camp", "chat", "fest"},
		},
		{
			name: "include_false_excluded",
			projects: map[string]ProjectEntry{
				"camp":    {Path: "projects/camp", Include: true},
				"archive": {Path: "projects/archive", Include: false},
			},
			wantNames: []string{"camp"},
		},
		{
			name: "missing_path_error",
			projects: map[string]ProjectEntry{
				"bad": {Path: "", Include: true},
			},
			wantErr: true,
		},
		{
			name: "sorted_output",
			projects: map[string]ProjectEntry{
				"zebra": {Path: "projects/zebra", Include: true},
				"alpha": {Path: "projects/alpha", Include: true},
				"mid":   {Path: "projects/mid", Include: true},
			},
			wantNames: []string{"alpha", "mid", "zebra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &LeverageConfig{Projects: tt.projects}
			got, err := ResolveProjects(context.Background(), root, cfg)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got) != len(tt.wantNames) {
				t.Fatalf("got %d projects, want %d", len(got), len(tt.wantNames))
			}
			for i, name := range tt.wantNames {
				if got[i].Name != name {
					t.Errorf("project[%d].Name = %q, want %q", i, got[i].Name, name)
				}
			}
		})
	}
}

func TestResolveProjects_MonorepoSplit(t *testing.T) {
	root := t.TempDir()

	cfg := &LeverageConfig{
		Projects: map[string]ProjectEntry{
			"obey": {
				Path:         "projects/obey-platform-monorepo/obey",
				Include:      true,
				InMonorepo:   true,
				MonorepoPath: "projects/obey-platform-monorepo",
			},
		},
	}

	got, err := ResolveProjects(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d projects, want 1", len(got))
	}

	proj := got[0]
	wantSCC := filepath.Join(root, "projects/obey-platform-monorepo/obey")
	wantGit := filepath.Join(root, "projects/obey-platform-monorepo")

	if proj.SCCDir != wantSCC {
		t.Errorf("SCCDir = %q, want %q", proj.SCCDir, wantSCC)
	}
	if proj.GitDir != wantGit {
		t.Errorf("GitDir = %q, want %q", proj.GitDir, wantGit)
	}
	if !proj.InMonorepo {
		t.Error("expected InMonorepo = true")
	}
}

func TestResolveProjects_GitRepoDefault(t *testing.T) {
	root := t.TempDir()

	cfg := &LeverageConfig{
		Projects: map[string]ProjectEntry{
			"camp": {Path: "projects/camp", Include: true},
		},
	}

	got, err := ResolveProjects(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d projects, want 1", len(got))
	}

	// When GitRepo is empty and not a monorepo, GitDir should equal SCCDir
	if got[0].GitDir != got[0].SCCDir {
		t.Errorf("GitDir = %q, want SCCDir = %q", got[0].GitDir, got[0].SCCDir)
	}
}

func TestResolveProjects_GitRepoOverride(t *testing.T) {
	root := t.TempDir()

	cfg := &LeverageConfig{
		Projects: map[string]ProjectEntry{
			"sub": {
				Path:    "projects/submodule/app",
				Include: true,
				GitRepo: "projects/submodule",
			},
		},
	}

	got, err := ResolveProjects(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantGit := filepath.Join(root, "projects/submodule")
	if got[0].GitDir != wantGit {
		t.Errorf("GitDir = %q, want %q", got[0].GitDir, wantGit)
	}
}

func TestResolveProjects_EmptyMapFallback(t *testing.T) {
	root := t.TempDir()

	// Set up a minimal campaign directory that project.List() can discover.
	// project.List looks for directories under projects/ that contain .git
	projDir := filepath.Join(root, "projects", "test-proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Initialize a git repo so project.List detects it
	cmd := exec.Command("git", "init", projDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	cfg := &LeverageConfig{} // nil Projects map

	got, err := ResolveProjects(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d projects, want 1", len(got))
	}
	if got[0].Name != "test-proj" {
		t.Errorf("Name = %q, want %q", got[0].Name, "test-proj")
	}
	if got[0].SCCDir != projDir {
		t.Errorf("SCCDir = %q, want %q", got[0].SCCDir, projDir)
	}
}

func TestResolveProjects_FallbackMonorepoExpansion(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create a monorepo with 2+ subprojects so project.List() expands it
	mono := filepath.Join(root, "projects", "my-mono")
	if err := os.MkdirAll(mono, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", mono)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Two subprojects with language markers
	for _, name := range []string{"svc-a", "svc-b"} {
		sub := filepath.Join(mono, name)
		os.MkdirAll(sub, 0o755)
		os.WriteFile(filepath.Join(sub, "go.mod"), []byte("module mono/"+name), 0644)
	}

	cfg := &LeverageConfig{} // empty Projects → fallback to project.List()

	got, err := ResolveProjects(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d projects, want 2", len(got))
	}

	for _, p := range got {
		if !p.InMonorepo {
			t.Errorf("%s: expected InMonorepo = true", p.Name)
		}
		wantGit := filepath.Join(root, "projects", "my-mono")
		if p.GitDir != wantGit {
			t.Errorf("%s: GitDir = %q, want %q", p.Name, p.GitDir, wantGit)
		}
		if p.SCCDir == p.GitDir {
			t.Errorf("%s: SCCDir should differ from GitDir for monorepo subproject", p.Name)
		}
	}
}

func TestResolveProjects_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := &LeverageConfig{
		Projects: map[string]ProjectEntry{
			"camp": {Path: "projects/camp", Include: true},
		},
	}

	_, err := ResolveProjects(ctx, t.TempDir(), cfg)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
}

func TestFilterByName(t *testing.T) {
	projects := []ResolvedProject{
		{Name: "alpha", SCCDir: "/a", GitDir: "/a"},
		{Name: "beta", SCCDir: "/b", GitDir: "/b"},
		{Name: "gamma", SCCDir: "/g", GitDir: "/g"},
	}

	tests := []struct {
		name      string
		filter    string
		wantCount int
		wantErr   bool
	}{
		{"empty filter returns all", "", 3, false},
		{"exact match", "beta", 1, false},
		{"not found", "missing", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FilterByName(projects, tt.filter)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Errorf("got %d projects, want %d", len(got), tt.wantCount)
			}
		})
	}
}
