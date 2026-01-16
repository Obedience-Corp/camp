package campaign

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDetect_Symlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test not supported on Windows")
	}

	// Create temp directory structure
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create real campaign
	realCampaign := filepath.Join(tmpDir, "real-campaign")
	if err := os.MkdirAll(filepath.Join(realCampaign, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// Create symlink to campaign
	symlinkPath := filepath.Join(tmpDir, "symlinked-campaign")
	if err := os.Symlink(realCampaign, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	ctx := context.Background()

	// Detection from symlink should return the resolved real path
	got, err := Detect(ctx, symlinkPath)
	if err != nil {
		t.Fatalf("Detect() from symlink error = %v", err)
	}

	if got != realCampaign {
		t.Errorf("Detect() from symlink = %v, want %v", got, realCampaign)
	}
}

func TestDetect_NestedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test not supported on Windows")
	}

	// Create temp directory structure
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create real campaign with nested structure
	realCampaign := filepath.Join(tmpDir, "real-campaign")
	nestedDir := filepath.Join(realCampaign, "projects", "foo")
	if err := os.MkdirAll(filepath.Join(realCampaign, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

	// Create symlink to nested directory
	symlinkPath := filepath.Join(tmpDir, "symlinked-nested")
	if err := os.Symlink(nestedDir, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	ctx := context.Background()

	// Detection from symlinked nested directory should find the real campaign
	got, err := Detect(ctx, symlinkPath)
	if err != nil {
		t.Fatalf("Detect() from nested symlink error = %v", err)
	}

	if got != realCampaign {
		t.Errorf("Detect() from nested symlink = %v, want %v", got, realCampaign)
	}
}

func TestDetect_NonCanonicalPath(t *testing.T) {
	// Create temp campaign
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	ctx := context.Background()

	// Test with path containing ..
	nonCanonicalPath := filepath.Join(campaignRoot, "subdir", "..")
	os.MkdirAll(filepath.Join(campaignRoot, "subdir"), 0755)

	got, err := Detect(ctx, nonCanonicalPath)
	if err != nil {
		t.Fatalf("Detect() with non-canonical path error = %v", err)
	}

	if got != campaignRoot {
		t.Errorf("Detect() with non-canonical path = %v, want %v", got, campaignRoot)
	}
}

func TestDetect_PathWithDot(t *testing.T) {
	// Create temp campaign
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	ctx := context.Background()

	// Test with path containing .
	pathWithDot := filepath.Join(campaignRoot, ".", "subdir", ".")
	os.MkdirAll(filepath.Join(campaignRoot, "subdir"), 0755)

	got, err := Detect(ctx, pathWithDot)
	if err != nil {
		t.Fatalf("Detect() with dot path error = %v", err)
	}

	if got != campaignRoot {
		t.Errorf("Detect() with dot path = %v, want %v", got, campaignRoot)
	}
}

func TestDetectWithTimeout(t *testing.T) {
	// Create temp campaign
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// Test successful detection with timeout
	got, err := DetectWithTimeout(campaignRoot)
	if err != nil {
		t.Fatalf("DetectWithTimeout() error = %v", err)
	}

	if got != campaignRoot {
		t.Errorf("DetectWithTimeout() = %v, want %v", got, campaignRoot)
	}
}

func TestDetectFromCwdWithTimeout(t *testing.T) {
	// Create temp campaign
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	if err := os.Chdir(campaignRoot); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	got, err := DetectFromCwdWithTimeout()
	if err != nil {
		t.Fatalf("DetectFromCwdWithTimeout() error = %v", err)
	}

	if got != campaignRoot {
		t.Errorf("DetectFromCwdWithTimeout() = %v, want %v", got, campaignRoot)
	}
}

func TestDetect_TimeoutExceeded(t *testing.T) {
	// Create a context that times out immediately
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait for timeout
	time.Sleep(1 * time.Millisecond)

	_, err := Detect(ctx, "/some/path")
	if err != context.DeadlineExceeded {
		t.Errorf("Detect() with expired timeout: got %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestDetect_PermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission test not reliable on Windows")
	}

	// Skip if running as root (permissions won't work)
	if os.Geteuid() == 0 {
		t.Skip("test requires non-root user")
	}

	// Create temp directory structure
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create campaign with restricted parent
	campaignRoot := filepath.Join(tmpDir, "campaign")
	restrictedDir := filepath.Join(campaignRoot, "restricted")
	nestedDir := filepath.Join(restrictedDir, "nested")

	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

	// Make the middle directory inaccessible
	// Note: We can't easily test this in the walk-up scenario because
	// we need to be able to access the nested directory first
	// This test at least verifies the code doesn't panic

	ctx := context.Background()

	// Detection from accessible nested directory should still work
	got, err := Detect(ctx, nestedDir)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	if got != campaignRoot {
		t.Errorf("Detect() = %v, want %v", got, campaignRoot)
	}
}

func BenchmarkDetectWithTimeout(b *testing.B) {
	tmpDir := b.TempDir()
	campaignRoot := filepath.Join(tmpDir, "campaign")
	campaignDir := filepath.Join(campaignRoot, CampaignDir)
	deepDir := filepath.Join(campaignRoot, "a", "b", "c", "d", "e")

	os.MkdirAll(campaignDir, 0755)
	os.MkdirAll(deepDir, 0755)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DetectWithTimeout(deepDir)
	}
}
