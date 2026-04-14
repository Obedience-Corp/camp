package intent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	intentcore "github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/paths"
)

// setupAddFlagsTest creates a campaign root and returns the service, path resolver,
// and config needed to test add flows end-to-end.
func setupAddFlagsTest(t *testing.T) (*intentcore.IntentService, string, *config.CampaignConfig, string) {
	t.Helper()

	root := t.TempDir()
	cfg := &config.CampaignConfig{
		ID:        "test-id",
		Name:      "test-campaign",
		Type:      config.CampaignTypeProduct,
		CreatedAt: time.Now(),
	}
	ctx := context.Background()
	if err := config.SaveCampaignConfig(ctx, root, cfg); err != nil {
		t.Fatalf("SaveCampaignConfig: %v", err)
	}
	jumps := config.DefaultJumpsConfig()
	if err := config.SaveJumpsConfig(ctx, root, &jumps); err != nil {
		t.Fatalf("SaveJumpsConfig: %v", err)
	}

	resolver := paths.NewResolverFromConfig(root, cfg)
	svc := intentcore.NewIntentService(root, resolver.Intents())
	if err := svc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}

	return svc, resolver.Intents(), cfg, root
}

func TestIntentAdd_WithBody(t *testing.T) {
	svc, intentsDir, cfg, root := setupAddFlagsTest(t)

	err := runFastCapture(context.Background(), svc, intentsDir, cfg, root, true, intentcore.CreateOptions{
		Title:  "Test with body",
		Type:   intentcore.TypeIdea,
		Author: "agent",
		Body:   "This is the body content",
	})
	if err != nil {
		t.Fatalf("runFastCapture: %v", err)
	}

	// Verify the intent was created with the body
	inbox := filepath.Join(intentsDir, "inbox")
	entries, err := os.ReadDir(inbox)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("inbox entries = %d, want 1", len(entries))
	}

	content, err := os.ReadFile(filepath.Join(inbox, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "This is the body content") {
		t.Fatalf("body not found in content: %s", content)
	}
}

func TestIntentAdd_WithConcept(t *testing.T) {
	svc, intentsDir, cfg, root := setupAddFlagsTest(t)

	err := runFastCapture(context.Background(), svc, intentsDir, cfg, root, true, intentcore.CreateOptions{
		Title:   "Test with concept",
		Type:    intentcore.TypeFeature,
		Author:  "agent",
		Concept: "projects/camp",
	})
	if err != nil {
		t.Fatalf("runFastCapture: %v", err)
	}

	inbox := filepath.Join(intentsDir, "inbox")
	entries, err := os.ReadDir(inbox)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("inbox entries = %d, want 1", len(entries))
	}

	content, err := os.ReadFile(filepath.Join(inbox, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "concept: projects/camp") {
		t.Fatalf("concept not found in content: %s", content)
	}
}

func TestIntentAdd_WithCustomAuthor(t *testing.T) {
	svc, intentsDir, cfg, root := setupAddFlagsTest(t)

	err := runFastCapture(context.Background(), svc, intentsDir, cfg, root, true, intentcore.CreateOptions{
		Title:  "Test custom author",
		Type:   intentcore.TypeIdea,
		Author: "lance",
	})
	if err != nil {
		t.Fatalf("runFastCapture: %v", err)
	}

	inbox := filepath.Join(intentsDir, "inbox")
	entries, err := os.ReadDir(inbox)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(inbox, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "author: lance") {
		t.Fatalf("custom author not found in content: %s", content)
	}
}

func TestIntentAdd_WithBodyFile(t *testing.T) {
	svc, intentsDir, cfg, root := setupAddFlagsTest(t)

	// Create a body file
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("Body from file\nwith multiple lines"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Read the body file content (simulating what resolveBody does)
	bodyContent, err := readBodySource(bodyFile)
	if err != nil {
		t.Fatalf("readBodySource: %v", err)
	}

	err = runFastCapture(context.Background(), svc, intentsDir, cfg, root, true, intentcore.CreateOptions{
		Title:  "Test body file",
		Type:   intentcore.TypeIdea,
		Author: "agent",
		Body:   bodyContent,
	})
	if err != nil {
		t.Fatalf("runFastCapture: %v", err)
	}

	inbox := filepath.Join(intentsDir, "inbox")
	entries, err := os.ReadDir(inbox)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(inbox, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "Body from file") {
		t.Fatalf("body file content not found: %s", content)
	}
}

func TestIntentAdd_FullAndBodyMutualExclusivity(t *testing.T) {
	// This tests the flag validation logic, not the full command
	cmd := newTestCmd()
	cmd.Flags().BoolP("full", "f", false, "")

	if err := cmd.Flags().Set("full", "true"); err != nil {
		t.Fatalf("Set(full) error: %v", err)
	}
	if err := cmd.Flags().Set("body", "some body"); err != nil {
		t.Fatalf("Set(body) error: %v", err)
	}

	fullMode, _ := cmd.Flags().GetBool("full")
	_, bodySet, err := resolveBody(cmd)
	if err != nil {
		t.Fatalf("resolveBody error: %v", err)
	}

	if fullMode && bodySet {
		// This is the expected condition - would produce an error in runIntentAdd
		return
	}
	t.Fatal("expected full + body to be detected as mutually exclusive")
}
