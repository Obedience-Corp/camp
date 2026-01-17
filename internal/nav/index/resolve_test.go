package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/obediencecorp/camp/internal/nav"
)

func TestResolve_NoCampaignRoot(t *testing.T) {
	_, err := Resolve(context.Background(), ResolveOptions{})
	if err == nil {
		t.Error("expected error for missing campaign root")
	}
}

func TestResolve_DirectCategory(t *testing.T) {
	// Create test campaign
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create projects directory
	projDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatalf("Failed to create projects: %v", err)
	}

	result, err := Resolve(context.Background(), ResolveOptions{
		CampaignRoot: root,
		Category:     nav.CategoryProjects,
		Query:        "",
	})

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.Path != projDir {
		t.Errorf("Path = %q, want %q", result.Path, projDir)
	}
	if !result.Exact {
		t.Error("Expected exact match for direct category")
	}
}

func TestResolve_CampaignRoot(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	result, err := Resolve(context.Background(), ResolveOptions{
		CampaignRoot: root,
		Category:     nav.CategoryAll,
		Query:        "",
	})

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.Path != root {
		t.Errorf("Path = %q, want %q", result.Path, root)
	}
}

func TestResolve_ExactMatch(t *testing.T) {
	// Create test campaign with projects
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create projects
	for _, name := range []string{"api-service", "web-app", "cli-tool"} {
		projPath := filepath.Join(root, "projects", name)
		if err := os.MkdirAll(projPath, 0755); err != nil {
			t.Fatalf("Failed to create project %s: %v", name, err)
		}
	}

	result, err := Resolve(context.Background(), ResolveOptions{
		CampaignRoot: root,
		Category:     nav.CategoryProjects,
		Query:        "api-service",
	})

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.Name != "api-service" {
		t.Errorf("Name = %q, want %q", result.Name, "api-service")
	}
	if !result.Exact {
		t.Error("Expected exact match")
	}
}

func TestResolve_FuzzyMatch(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create projects
	for _, name := range []string{"api-service", "web-app", "cli-tool"} {
		projPath := filepath.Join(root, "projects", name)
		if err := os.MkdirAll(projPath, 0755); err != nil {
			t.Fatalf("Failed to create project %s: %v", name, err)
		}
	}

	result, err := Resolve(context.Background(), ResolveOptions{
		CampaignRoot: root,
		Category:     nav.CategoryProjects,
		Query:        "api",
	})

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.Name != "api-service" {
		t.Errorf("Name = %q, want %q", result.Name, "api-service")
	}
	if result.Exact {
		t.Error("Expected fuzzy match, not exact")
	}
}

func TestResolve_NoMatches(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create a project
	projPath := filepath.Join(root, "projects", "api-service")
	if err := os.MkdirAll(projPath, 0755); err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	_, err := Resolve(context.Background(), ResolveOptions{
		CampaignRoot: root,
		Category:     nav.CategoryProjects,
		Query:        "xyz",
	})

	if err == nil {
		t.Error("expected error for no matches")
	}
}

func TestResolve_ExactOnly(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	projPath := filepath.Join(root, "projects", "api-service")
	if err := os.MkdirAll(projPath, 0755); err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// Should fail - "api" doesn't exactly match "api-service"
	_, err := Resolve(context.Background(), ResolveOptions{
		CampaignRoot: root,
		Category:     nav.CategoryProjects,
		Query:        "api",
		ExactOnly:    true,
	})

	if err == nil {
		t.Error("expected error for exact only with fuzzy query")
	}
}

func TestResolve_MultipleMatches(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create projects that all match "api"
	for _, name := range []string{"api-service", "api-gateway", "api-docs"} {
		projPath := filepath.Join(root, "projects", name)
		if err := os.MkdirAll(projPath, 0755); err != nil {
			t.Fatalf("Failed to create project %s: %v", name, err)
		}
	}

	result, err := Resolve(context.Background(), ResolveOptions{
		CampaignRoot: root,
		Category:     nav.CategoryProjects,
		Query:        "api",
	})

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if !result.HasMultipleMatches() {
		t.Error("expected multiple matches")
	}

	if result.MatchCount() < 2 {
		t.Errorf("MatchCount = %d, want >= 2", result.MatchCount())
	}
}

func TestResolve_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := Resolve(ctx, ResolveOptions{
		CampaignRoot: "/tmp",
		Category:     nav.CategoryProjects,
		Query:        "test",
	})

	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestResolve_EmptyCategory(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create empty projects directory
	projDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatalf("Failed to create projects: %v", err)
	}

	_, err := Resolve(context.Background(), ResolveOptions{
		CampaignRoot: root,
		Category:     nav.CategoryProjects,
		Query:        "test",
	})

	if err == nil {
		t.Error("expected error for empty category")
	}
}

func TestResolvePath(t *testing.T) {
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	projPath := filepath.Join(root, "projects", "my-app")
	if err := os.MkdirAll(projPath, 0755); err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	path, err := ResolvePath(context.Background(), root, nav.CategoryProjects, "my-app")
	if err != nil {
		t.Fatalf("ResolvePath failed: %v", err)
	}

	if path != projPath {
		t.Errorf("Path = %q, want %q", path, projPath)
	}
}

func TestResolveResult_Methods(t *testing.T) {
	t.Run("no matches", func(t *testing.T) {
		r := &ResolveResult{}
		if r.HasMultipleMatches() {
			t.Error("expected no multiple matches")
		}
		if r.MatchCount() != 0 {
			t.Errorf("MatchCount = %d, want 0", r.MatchCount())
		}
	})

	t.Run("single match", func(t *testing.T) {
		r := &ResolveResult{
			Matches: []Target{{Name: "test"}},
		}
		if r.HasMultipleMatches() {
			t.Error("expected no multiple matches for single")
		}
		if r.MatchCount() != 1 {
			t.Errorf("MatchCount = %d, want 1", r.MatchCount())
		}
	})

	t.Run("multiple matches", func(t *testing.T) {
		r := &ResolveResult{
			Matches: []Target{{Name: "test1"}, {Name: "test2"}},
		}
		if !r.HasMultipleMatches() {
			t.Error("expected multiple matches")
		}
		if r.MatchCount() != 2 {
			t.Errorf("MatchCount = %d, want 2", r.MatchCount())
		}
	})
}

// Benchmarks

func BenchmarkResolve_Direct(b *testing.B) {
	root := b.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		b.Fatalf("Failed to create .campaign: %v", err)
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = Resolve(ctx, ResolveOptions{
			CampaignRoot: root,
			Category:     nav.CategoryProjects,
			Query:        "",
		})
	}
}

func BenchmarkResolve_Fuzzy(b *testing.B) {
	root := b.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		b.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create multiple projects
	for i := 0; i < 50; i++ {
		name := filepath.Join(root, "projects", string(rune('a'+i%26))+"-project")
		if err := os.MkdirAll(name, 0755); err != nil {
			b.Fatalf("Failed to create project: %v", err)
		}
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = Resolve(ctx, ResolveOptions{
			CampaignRoot: root,
			Category:     nav.CategoryProjects,
			Query:        "proj",
		})
	}
}
