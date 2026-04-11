package complete

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// campaignYAML is a minimal campaign.yaml with default shortcuts
const campaignYAML = `id: test-campaign-id
name: test-campaign
type: product
shortcuts:
  p:
    path: "projects/"
    description: "Jump to projects directory"
  pw:
    path: "projects/worktrees/"
    description: "Jump to project worktrees"
  f:
    path: "festivals/"
    description: "Jump to festivals directory"
  a:
    path: "ai_docs/"
    description: "Jump to AI docs directory"
  d:
    path: "docs/"
    description: "Jump to docs directory"
  du:
    path: "dungeon/"
    description: "Jump to dungeon directory"
  w:
    path: "workflow/"
    description: "Jump to workflow directory"
  cr:
    path: "workflow/code_reviews/"
    description: "Jump to code reviews"
  pi:
    path: "workflow/pipelines/"
    description: "Jump to pipelines"
  de:
    path: "workflow/design/"
    description: "Jump to design"
  i:
    path: ".campaign/intents/"
    description: "Jump to intents"
`

// createTestCampaign creates a test campaign with shortcuts configured.
func createTestCampaign(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	campDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign: %v", err)
	}

	// Create campaign.yaml with shortcuts
	campaignPath := filepath.Join(campDir, "campaign.yaml")
	if err := os.WriteFile(campaignPath, []byte(campaignYAML), 0644); err != nil {
		t.Fatalf("Failed to create campaign.yaml: %v", err)
	}

	return root
}

func TestGenerate_NoArgs(t *testing.T) {
	// Create test campaign with shortcuts
	root := createTestCampaign(t)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(oldWd)

	if err := os.Chdir(root); err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}

	ctx := context.Background()
	candidates, err := Generate(ctx, nil)

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should return category shortcuts from config (11 shortcut keys + 11 path concept names)
	if len(candidates) != 22 {
		t.Errorf("Got %d candidates, want 22", len(candidates))
	}
}

func TestGenerate_NoArgs_NotInCampaign(t *testing.T) {
	// Test outside a campaign - should return nil
	root := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(oldWd)

	if err := os.Chdir(root); err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}

	ctx := context.Background()
	candidates, err := Generate(ctx, nil)

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should return nil when not in a campaign
	if candidates != nil {
		t.Errorf("Got %d candidates, want nil", len(candidates))
	}
}

func TestGenerate_EmptyArgs(t *testing.T) {
	// Create test campaign with shortcuts
	root := createTestCampaign(t)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(oldWd)

	if err := os.Chdir(root); err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}

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
	// Create test campaign with shortcuts
	root := createTestCampaign(t)

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
	// Create test campaign with shortcuts
	root := createTestCampaign(t)

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
	// Create test campaign with shortcuts
	root := createTestCampaign(t)

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
	// Create test campaign with shortcuts
	root := createTestCampaign(t)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	shortcuts := CategoryShortcuts()

	// Should have 22 entries (11 shortcut keys + 11 path concept names)
	if len(shortcuts) != 22 {
		t.Errorf("Got %d shortcuts, want 22", len(shortcuts))
	}
}

func TestCategoryShortcuts_NotInCampaign(t *testing.T) {
	// Test outside a campaign
	root := t.TempDir()

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	shortcuts := CategoryShortcuts()

	// Should return nil when not in a campaign
	if shortcuts != nil {
		t.Errorf("Got %d shortcuts, want nil", len(shortcuts))
	}
}

func TestGenerate_Performance(t *testing.T) {
	// Create test campaign with shortcuts
	root := createTestCampaign(t)

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

	// Should return empty when not in campaign (no shortcuts available)
	if len(candidates) != 0 {
		t.Errorf("Got %d candidates, want 0 (not in campaign)", len(candidates))
	}
}

func TestGenerate_ContextCancellation(t *testing.T) {
	// Create test campaign with shortcuts
	root := createTestCampaign(t)

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
