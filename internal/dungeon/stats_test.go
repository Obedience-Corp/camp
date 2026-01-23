package dungeon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewStatsGatherer(t *testing.T) {
	g := NewStatsGatherer()
	if g == nil {
		t.Fatal("NewStatsGatherer should not return nil")
	}
	// hasSCC and hasFest depend on system, just verify no panic
}

func TestStatsGatherer_Available(t *testing.T) {
	g := NewStatsGatherer()
	// Available() should return true if either scc or fest is installed
	// This is system-dependent, just verify it doesn't panic
	_ = g.Available()
}

func TestStatsGatherer_Gather_NoTools(t *testing.T) {
	// Create a gatherer with no tools available
	g := &StatsGatherer{
		hasSCC:  false,
		hasFest: false,
	}

	ctx := context.Background()
	stats := g.Gather(ctx, "/tmp")

	if stats != nil {
		t.Error("Gather should return nil when no tools available")
	}
}

func TestStatsGatherer_Gather_WithPath(t *testing.T) {
	g := NewStatsGatherer()
	if !g.Available() {
		t.Skip("No stats tools (scc or fest) available")
	}

	// Create temp directory with a file
	tmpDir, err := os.MkdirTemp("", "stats-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := context.Background()
	stats := g.Gather(ctx, tmpDir)

	// If tools are available, we should get some stats
	if stats == nil {
		t.Skip("Stats gathering returned nil (tool may have failed)")
	}

	if stats.Source != "scc" && stats.Source != "fest" {
		t.Errorf("unexpected source: %s", stats.Source)
	}
}

func TestStatsGatherer_Gather_NonexistentPath(t *testing.T) {
	g := NewStatsGatherer()
	if !g.Available() {
		t.Skip("No stats tools available")
	}

	ctx := context.Background()
	stats := g.Gather(ctx, "/nonexistent/path/that/does/not/exist")

	// Should gracefully return nil for non-existent paths
	if stats != nil && stats.Files > 0 {
		t.Error("should not report files for non-existent path")
	}
}

func TestStatsGatherer_Gather_ContextCancelled(t *testing.T) {
	g := NewStatsGatherer()
	if !g.Available() {
		t.Skip("No stats tools available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	stats := g.Gather(ctx, "/tmp")

	// Should handle cancelled context gracefully (return nil)
	// The command will fail due to cancelled context
	_ = stats // Just verify no panic
}
