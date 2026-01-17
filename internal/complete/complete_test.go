package complete

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerate_NoArgs(t *testing.T) {
	ctx := context.Background()
	candidates, err := Generate(ctx, nil)

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should return category shortcuts
	expected := []string{"p", "c", "f", "a", "d", "w", "r", "pi"}
	if len(candidates) != len(expected) {
		t.Errorf("Got %d candidates, want %d", len(candidates), len(expected))
	}

	for i, c := range candidates {
		if c != expected[i] {
			t.Errorf("candidate[%d] = %q, want %q", i, c, expected[i])
		}
	}
}

func TestGenerate_EmptyArgs(t *testing.T) {
	ctx := context.Background()
	candidates, err := Generate(ctx, []string{})

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(candidates) == 0 {
		t.Error("expected category shortcuts for empty args")
	}
}

func TestGenerate_CategoryShortcut(t *testing.T) {
	// Create test campaign
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create projects
	for _, name := range []string{"api-service", "web-app", "cli-tool"} {
		projPath := filepath.Join(root, "projects", name)
		if err := os.MkdirAll(projPath, 0755); err != nil {
			t.Fatalf("Failed to create project: %v", err)
		}
	}

	// Change to campaign root for test
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(oldWd)

	if err := os.Chdir(root); err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}

	ctx := context.Background()
	candidates, err := Generate(ctx, []string{"p"})

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should return project names
	if len(candidates) != 3 {
		t.Errorf("Got %d candidates, want 3", len(candidates))
	}
}

func TestGenerate_CategoryWithQuery(t *testing.T) {
	// Create test campaign
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create projects
	for _, name := range []string{"api-service", "api-gateway", "web-app"} {
		projPath := filepath.Join(root, "projects", name)
		if err := os.MkdirAll(projPath, 0755); err != nil {
			t.Fatalf("Failed to create project: %v", err)
		}
	}

	// Change to campaign root
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()
	candidates, err := Generate(ctx, []string{"p", "api"})

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should return api* projects
	if len(candidates) < 2 {
		t.Errorf("Got %d candidates, want at least 2", len(candidates))
	}
}

func TestGenerate_PartialShortcut(t *testing.T) {
	// Create test campaign
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	projPath := filepath.Join(root, "projects", "test-project")
	if err := os.MkdirAll(projPath, 0755); err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()

	// "p" is a valid shortcut
	candidates, err := Generate(ctx, []string{"p"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should get project names
	if len(candidates) == 0 {
		t.Error("expected some candidates")
	}
}

func TestGenerate_Timeout(t *testing.T) {
	// Create a cancelled context to simulate timeout
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should not error, just return empty or partial
	candidates, _ := Generate(ctx, []string{"nonexistent"})

	// Should return shortcuts that match or empty
	_ = candidates
}

func TestCategoryShortcuts(t *testing.T) {
	shortcuts := CategoryShortcuts()

	expected := []string{"p", "c", "f", "a", "d", "w", "r", "pi"}

	if len(shortcuts) != len(expected) {
		t.Errorf("Got %d shortcuts, want %d", len(shortcuts), len(expected))
	}

	for i, s := range shortcuts {
		if s != expected[i] {
			t.Errorf("shortcut[%d] = %q, want %q", i, s, expected[i])
		}
	}
}

func TestGenerate_Performance(t *testing.T) {
	// Create test campaign
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create many projects
	for i := 0; i < 100; i++ {
		name := filepath.Join(root, "projects", string(rune('a'+i%26))+"-project")
		if err := os.MkdirAll(name, 0755); err != nil {
			t.Fatalf("Failed to create project: %v", err)
		}
	}

	// Change to campaign root
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()

	start := time.Now()
	_, err := Generate(ctx, []string{"p"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should complete within a reasonable time (allow some margin for CI)
	if elapsed > 200*time.Millisecond {
		t.Errorf("Generate took %v, want < 200ms", elapsed)
	}
}

func TestGenerate_NotInCampaign(t *testing.T) {
	// Create a directory that is not a campaign
	root := t.TempDir()

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()
	candidates, _ := Generate(ctx, []string{"p"})

	// Should still return shortcuts when not in campaign
	// (or empty, which is also acceptable)
	_ = candidates
}

func TestGenerate_ContextCancellation(t *testing.T) {
	// Create test campaign
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	projPath := filepath.Join(root, "projects", "test")
	if err := os.MkdirAll(projPath, 0755); err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should not panic, just return partial or empty
	candidates, _ := Generate(ctx, []string{"p"})
	_ = candidates
}

// Benchmarks

func BenchmarkGenerate_NoArgs(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = Generate(ctx, nil)
	}
}

func BenchmarkGenerate_CategoryShortcut(b *testing.B) {
	root := b.TempDir()
	campDir := filepath.Join(root, ".campaign")
	os.MkdirAll(campDir, 0755)

	for i := 0; i < 50; i++ {
		name := filepath.Join(root, "projects", string(rune('a'+i%26))+"-project")
		os.MkdirAll(name, 0755)
	}

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = Generate(ctx, []string{"p"})
	}
}
