package leverage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/project"
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

	// Create a repo with .gitmodules listing 2 submodules
	mono := filepath.Join(root, "projects", "my-mono")
	if err := os.MkdirAll(mono, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", mono)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Write .gitmodules declaring two submodules
	gitmodules := ""
	for _, name := range []string{"svc-a", "svc-b"} {
		gitmodules += fmt.Sprintf("[submodule %q]\n\tpath = %s\n\turl = https://example.com/%s.git\n", name, name, name)
		sub := filepath.Join(mono, name)
		os.MkdirAll(sub, 0o755)
		os.WriteFile(filepath.Join(sub, "go.mod"), []byte("module mono/"+name), 0644)
	}
	os.WriteFile(filepath.Join(mono, ".gitmodules"), []byte(gitmodules), 0644)

	cfg := &LeverageConfig{} // empty Projects → fallback to project.List()

	got, err := ResolveProjects(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 3: root entry + 2 submodule entries
	if len(got) != 3 {
		names := make([]string, len(got))
		for i, p := range got {
			names[i] = p.Name
		}
		t.Fatalf("got %d projects %v, want 3 (root + 2 submodules)", len(got), names)
	}

	// Check submodule entries
	for _, p := range got {
		if !p.InMonorepo {
			// Root entry should NOT be InMonorepo
			if p.Name != "my-mono" {
				t.Errorf("%s: expected InMonorepo = true", p.Name)
			}
			continue
		}
		wantGit := filepath.Join(root, "projects", "my-mono")
		if p.GitDir != wantGit {
			t.Errorf("%s: GitDir = %q, want %q", p.Name, p.GitDir, wantGit)
		}
		if p.SCCDir == p.GitDir {
			t.Errorf("%s: SCCDir should differ from GitDir for monorepo subproject", p.Name)
		}
	}

	// Check root entry has ExcludeDirs
	rootEntry := got[0] // sorted alphabetically, "my-mono" comes first
	if rootEntry.Name != "my-mono" {
		// Find the root entry
		for _, p := range got {
			if p.Name == "my-mono" {
				rootEntry = p
				break
			}
		}
	}
	if len(rootEntry.ExcludeDirs) != 2 {
		t.Errorf("root ExcludeDirs = %v, want 2 entries", rootEntry.ExcludeDirs)
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

// TestDeduplicateProjectsForLeverage covers the leverage-only dedup that keeps
// one scoring entry per repository URL.
//
// This is the regression fix for cases where the campaign contains both
// `projects/foo` and `projects/<monorepo>/foo` (via .gitmodules), or contains
// the same submodule repository in multiple monorepos. Leverage must score that
// work once, not once per checkout path.
func TestDeduplicateProjectsForLeverage(t *testing.T) {
	const sharedURL = "git@github.com:test/foo.git"

	cases := []struct {
		name      string
		input     []project.Project
		wantNames []string
	}{
		{
			name: "shadows submodule when standalone with same URL exists",
			input: []project.Project{
				{Name: "foo", URL: sharedURL},
				{Name: "mono", URL: "git@github.com:test/mono.git"},
				{Name: "mono@foo", URL: sharedURL, MonorepoRoot: "projects/mono"},
				{Name: "mono@bar", URL: "git@github.com:test/bar.git", MonorepoRoot: "projects/mono"},
			},
			wantNames: []string{"foo", "mono", "mono@bar"},
		},
		{
			name: "keeps one submodule when same URL appears in multiple monorepos",
			input: []project.Project{
				{Name: "mono-a", URL: "git@github.com:test/mono-a.git"},
				{Name: "mono-a@foo", URL: sharedURL, MonorepoRoot: "projects/mono-a"},
				{Name: "mono-b", URL: "git@github.com:test/mono-b.git"},
				{Name: "mono-b@foo", URL: sharedURL, MonorepoRoot: "projects/mono-b"},
			},
			wantNames: []string{"mono-a", "mono-a@foo", "mono-b"},
		},
		{
			name: "standalone wins even when discovered after submodule",
			input: []project.Project{
				{Name: "mono@foo", URL: sharedURL, MonorepoRoot: "projects/mono"},
				{Name: "foo", URL: sharedURL},
			},
			wantNames: []string{"foo"},
		},
		{
			name: "keeps submodule with empty URL even if a standalone shares the URL",
			input: []project.Project{
				{Name: "foo", URL: sharedURL},
				// Submodule with no URL (uninitialised, no remote).
				// Drop is keyed on URL match, so an empty-URL
				// submodule cannot be shadowed.
				{Name: "mono@orphan", URL: "", MonorepoRoot: "projects/mono"},
			},
			wantNames: []string{"foo", "mono@orphan"},
		},
		{
			name: "no-op when no submodule entries present",
			input: []project.Project{
				{Name: "foo", URL: sharedURL},
				{Name: "bar", URL: "git@github.com:test/bar.git"},
			},
			wantNames: []string{"foo", "bar"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := deduplicateProjectsForLeverage(tt.input)

			gotNames := make([]string, len(got))
			for i, p := range got {
				gotNames[i] = p.Name
			}

			if len(gotNames) != len(tt.wantNames) {
				t.Fatalf("got %v, want %v", gotNames, tt.wantNames)
			}
			for i, want := range tt.wantNames {
				if gotNames[i] != want {
					t.Errorf("idx %d: got %q, want %q (full: %v)", i, gotNames[i], want, gotNames)
				}
			}
		})
	}
}

// TestResolveProjects_FallbackDropsSubmoduleShadowedByStandalone is
// the end-to-end regression test for the "double-counting" bug
// reported in issue #263. A campaign with both `projects/child` and
// `projects/mono` (where mono lists child as a .gitmodules entry
// pointing at the same remote URL) must surface only the standalone
// child to leverage scoring.
func TestResolveProjects_FallbackDropsSubmoduleShadowedByStandalone(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	const childURL = "git@github.com:test/child.git"

	projectsDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Standalone child clone.
	standaloneChild := filepath.Join(projectsDir, "child")
	if err := os.MkdirAll(standaloneChild, 0o755); err != nil {
		t.Fatal(err)
	}
	initLeverageRepo(t, standaloneChild, childURL)

	// Monorepo containing a "child" submodule whose remote matches.
	mono := filepath.Join(projectsDir, "mono")
	if err := os.MkdirAll(mono, 0o755); err != nil {
		t.Fatal(err)
	}
	initLeverageRepo(t, mono, "git@github.com:test/mono.git")

	subChild := filepath.Join(mono, "child")
	if err := os.MkdirAll(subChild, 0o755); err != nil {
		t.Fatal(err)
	}
	initLeverageRepo(t, subChild, childURL)

	// .gitmodules entry so project.List sees `mono@child` as a
	// monorepo subproject.
	gitmodules := fmt.Sprintf("[submodule %q]\n\tpath = child\n\turl = %s\n", "child", childURL)
	if err := os.WriteFile(filepath.Join(mono, ".gitmodules"), []byte(gitmodules), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &LeverageConfig{}
	got, err := ResolveProjects(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("ResolveProjects error = %v", err)
	}

	names := make([]string, len(got))
	for i, p := range got {
		names[i] = p.Name
	}

	for _, n := range names {
		if n == "mono@child" {
			t.Fatalf("mono@child should be dropped (shadowed by standalone child); got %v", names)
		}
	}
	hasStandalone := false
	hasMono := false
	for _, n := range names {
		switch n {
		case "child":
			hasStandalone = true
		case "mono":
			hasMono = true
		}
	}
	if !hasStandalone {
		t.Errorf("standalone 'child' missing from result: %v", names)
	}
	if !hasMono {
		t.Errorf("monorepo root 'mono' missing from result: %v", names)
	}
}

// initLeverageRepo initialises a git repo at path with the given
// remote URL and creates one commit so commands like
// `git remote get-url origin` and `git log` work.
func initLeverageRepo(t *testing.T, path, remoteURL string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", path},
		{"git", "-C", path, "remote", "add", "origin", remoteURL},
		{"git", "-C", path, "config", "user.email", "test@test.com"},
		{"git", "-C", path, "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("command %v failed: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "-C", path, "add", "."},
		{"git", "-C", path, "commit", "-m", "seed"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("command %v failed: %v\n%s", args, err, out)
		}
	}
}
