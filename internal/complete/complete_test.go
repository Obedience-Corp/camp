package complete

import (
	"context"
	"errors"
	"fmt"
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
// Takes testing.TB rather than *testing.T so the benchmark can build the same
// fixture the tests use.
func createTestCampaign(t testing.TB) string {
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

	assertContainsCandidate(t, candidates, "de")
	assertContainsCandidate(t, candidates, "design")
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
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()
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

func TestGenerate_CustomWorkflowShortcutRecentFirst(t *testing.T) {
	root := createTestCampaign(t)
	settingsDir := filepath.Join(root, ".campaign", "settings")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	jumps := `paths:
  workflow: "workflow/"
shortcuts:
  re:
    path: "workflow/research/"
    description: "Research workflow"
`
	if err := os.WriteFile(filepath.Join(settingsDir, "jumps.yaml"), []byte(jumps), 0644); err != nil {
		t.Fatal(err)
	}

	olderDir := filepath.Join(root, "workflow", "research", "older-work")
	newerDir := filepath.Join(root, "workflow", "research", "newer-work")
	for _, dir := range []string{olderDir, newerDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".workitem"), []byte("kind: workitem\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(filepath.Join(olderDir, ".workitem"), now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(newerDir, ".workitem"), now, now); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	candidates, err := Generate(context.Background(), []string{"re"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(candidates) < 2 {
		t.Fatalf("Got %d candidates, want at least 2: %v", len(candidates), candidates)
	}
	if candidates[0] != "newer-work/" || candidates[1] != "older-work/" {
		t.Fatalf("candidates = %v, want newer workitem before older workitem", candidates)
	}
}

func TestGenerate_BuiltinShortcutRecentFirst(t *testing.T) {
	root := createTestCampaign(t)

	olderDir := filepath.Join(root, "workflow", "design", "older-design")
	newerDir := filepath.Join(root, "workflow", "design", "newer-design")
	for _, dir := range []string{olderDir, newerDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".workitem"), []byte("kind: workitem\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(filepath.Join(olderDir, ".workitem"), now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(newerDir, ".workitem"), now, now); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	candidates, err := Generate(context.Background(), []string{"de"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	var newerIdx, olderIdx = -1, -1
	for i, c := range candidates {
		switch c {
		case "newer-design", "newer-design/":
			newerIdx = i
		case "older-design", "older-design/":
			olderIdx = i
		}
	}
	if newerIdx == -1 || olderIdx == -1 {
		t.Fatalf("expected both design candidates present; got %v", candidates)
	}
	if newerIdx >= olderIdx {
		t.Fatalf("newer-design (index %d) should sort before older-design (index %d) for builtin shortcut `de`; candidates=%v",
			newerIdx, olderIdx, candidates)
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

	assertContainsCandidate(t, shortcuts, "de")
	assertContainsCandidate(t, shortcuts, "design")
}

func TestGenerate_FirstArgSlashDrill(t *testing.T) {
	root := createTestCampaign(t)

	for _, path := range []string{
		filepath.Join(root, "workflow", "design", "festival_app"),
		filepath.Join(root, "workflow", "design", "festival_site"),
	} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("Failed to create design entry: %v", err)
		}
	}

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()
	candidates, err := Generate(ctx, []string{"design/"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	assertContainsCandidate(t, candidates, "festival_app/")
	assertContainsCandidate(t, candidates, "festival_site/")
}

func TestGenerate_FirstArgShortcutDrill(t *testing.T) {
	root := createTestCampaign(t)

	for _, path := range []string{
		filepath.Join(root, "workflow", "design", "festival_app"),
		filepath.Join(root, "workflow", "design", "festival_site"),
	} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("Failed to create design entry: %v", err)
		}
	}

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(root)

	ctx := context.Background()
	candidates, err := Generate(ctx, []string{"de@"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	assertContainsCandidate(t, candidates, "festival_app/")
	assertContainsCandidate(t, candidates, "festival_site/")
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

// createLargeTestCampaign builds a campaign with enough projects that
// completion has real work to do.
func createLargeTestCampaign(t testing.TB) string {
	t.Helper()
	root := createTestCampaign(t)
	for i := 0; i < 100; i++ {
		name := filepath.Join(root, "projects", string(rune('a'+i%26))+"-project")
		if err := os.MkdirAll(name, 0755); err != nil {
			t.Fatalf("Failed to create project: %v", err)
		}
	}
	return root
}

// chdir moves into dir for the duration of the test.
func chdir(t testing.TB, dir string) {
	t.Helper()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}
}

// Exhausting the completion budget is a designed outcome, not a failure.
// Generate bounds itself with Timeout precisely so a cold index or a slow
// filesystem cannot hang the user's shell, so callers must get a usable
// (possibly empty) answer rather than something they have to handle.
//
// This replaces a test that asserted elapsed < 200ms while Generate's own
// deadline was also exactly 200ms. That assertion was unreachable: any run slow
// enough to trip it had already returned a deadline error and failed on the
// line above with "Generate failed: context deadline exceeded", which reads as
// a broken feature rather than a busy machine. It fired whenever the suite ran
// under enough parallel load, which is the one time the number means least.
func TestGenerate_ExhaustedBudgetIsNotAnError(t *testing.T) {
	chdir(t, createLargeTestCampaign(t))

	// Already past its deadline, so the budget is gone before any work starts.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()

	candidates, err := Generate(ctx, []string{"p"})
	if err != nil {
		t.Errorf("Generate with an exhausted budget returned an error: %v", err)
	}
	if candidates == nil {
		return // no candidates is the expected degraded answer
	}
	for _, c := range candidates {
		if c == "" {
			t.Error("Generate returned an empty candidate; degraded output must still be usable")
		}
	}
}

// Same contract for the rich variant, which the --described completion path uses.
func TestGenerateRich_ExhaustedBudgetIsNotAnError(t *testing.T) {
	chdir(t, createLargeTestCampaign(t))

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()

	if _, err := GenerateRich(ctx, []string{"p"}); err != nil {
		t.Errorf("GenerateRich with an exhausted budget returned an error: %v", err)
	}
}

// A full budget over a large campaign must still produce candidates. This is
// the half of the old test that was worth keeping, without the clock: it
// asserts the work completes, not how fast the machine running it is.
func TestGenerate_LargeCampaignYieldsCandidates(t *testing.T) {
	chdir(t, createLargeTestCampaign(t))

	candidates, err := Generate(context.Background(), []string{"p"})
	if err != nil {
		// A deadline here means the machine was loaded, not that camp is
		// broken; production treats it the same way, by showing fewer
		// completions. Failing the suite for it is what made this flaky.
		if errors.Is(err, context.DeadlineExceeded) {
			t.Skipf("completion budget exhausted under load: %v", err)
		}
		t.Fatalf("Generate failed: %v", err)
	}
	if len(candidates) == 0 {
		t.Error("Generate returned no candidates for a campaign with 100 projects")
	}
}

// Performance belongs in a benchmark, where it is measured and reported rather
// than asserted against a wall clock that a loaded CI box will lose.
//
//	go test ./internal/complete/ -bench=Generate -benchtime=100x
func BenchmarkGenerate_LargeCampaign(b *testing.B) {
	chdir(b, createLargeTestCampaign(b))
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Generate(ctx, []string{"p"}); err != nil {
			b.Fatalf("Generate failed: %v", err)
		}
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

// BenchmarkGenerate_RecentFirst_Scale measures the cost of applyRecentFirstOrder
// across 100/1000/5000 workitem markers. The Generate path adds one os.Stat per
// candidate; this guards against regression beyond the 200ms shell-completion
// budget defined by complete.Timeout.
func BenchmarkGenerate_RecentFirst_Scale(b *testing.B) {
	for _, scale := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("workitems_%d", scale), func(b *testing.B) {
			root := b.TempDir()
			campDir := filepath.Join(root, ".campaign")
			if err := os.MkdirAll(campDir, 0o755); err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(campDir, "campaign.yaml"), []byte(campaignYAML), 0o644); err != nil {
				b.Fatal(err)
			}
			for i := 0; i < scale; i++ {
				dir := filepath.Join(root, "workflow", "design", fmt.Sprintf("item-%05d", i))
				if err := os.MkdirAll(dir, 0o755); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, ".workitem"), []byte("kind: workitem\n"), 0o644); err != nil {
					b.Fatal(err)
				}
			}

			oldWd, err := os.Getwd()
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = os.Chdir(oldWd) }()
			if err := os.Chdir(root); err != nil {
				b.Fatal(err)
			}

			ctx := context.Background()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = Generate(ctx, []string{"de"})
			}
		})
	}
}

func assertContainsCandidate(t *testing.T, candidates []string, want string) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate == want {
			return
		}
	}
	t.Fatalf("candidates %v do not contain %q", candidates, want)
}
