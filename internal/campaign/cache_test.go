package campaign

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestDetectCached(t *testing.T) {
	// Create temp campaign
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "test-campaign")
	campaignDir := filepath.Join(campaignRoot, CampaignDir)

	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	// Clear cache before test
	ClearCache()

	// Change to campaign root
	if err := os.Chdir(campaignRoot); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	ctx := context.Background()

	// First call - cache miss
	if IsCached() {
		t.Error("IsCached() should be false before first call")
	}

	root1, err := DetectCached(ctx)
	if err != nil {
		t.Fatalf("DetectCached() error = %v", err)
	}
	if root1 != campaignRoot {
		t.Errorf("DetectCached() = %v, want %v", root1, campaignRoot)
	}

	// Second call - cache hit
	if !IsCached() {
		t.Error("IsCached() should be true after detection")
	}

	root2, err := DetectCached(ctx)
	if err != nil {
		t.Fatalf("DetectCached() second call error = %v", err)
	}
	if root2 != root1 {
		t.Errorf("DetectCached() second call = %v, want %v", root2, root1)
	}
}

func TestCacheInvalidation(t *testing.T) {
	// Create two temp campaigns
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaign1 := filepath.Join(tmpDir, "campaign1")
	campaign2 := filepath.Join(tmpDir, "campaign2")

	if err := os.MkdirAll(filepath.Join(campaign1, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign1: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(campaign2, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign2: %v", err)
	}

	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	// Clear cache before test
	ClearCache()

	ctx := context.Background()

	// Detect in campaign1
	if err := os.Chdir(campaign1); err != nil {
		t.Fatalf("failed to change to campaign1: %v", err)
	}
	root1, err := DetectCached(ctx)
	if err != nil {
		t.Fatalf("DetectCached() in campaign1 error = %v", err)
	}
	if root1 != campaign1 {
		t.Errorf("DetectCached() in campaign1 = %v, want %v", root1, campaign1)
	}

	// Change to campaign2 - cache should be invalidated
	if err := os.Chdir(campaign2); err != nil {
		t.Fatalf("failed to change to campaign2: %v", err)
	}

	// Cache should be invalid now (different cwd)
	root2, err := DetectCached(ctx)
	if err != nil {
		t.Fatalf("DetectCached() in campaign2 error = %v", err)
	}
	if root2 != campaign2 {
		t.Errorf("DetectCached() in campaign2 = %v, want %v", root2, campaign2)
	}
}

func TestClearCache(t *testing.T) {
	// Create temp campaign
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "test-campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	ClearCache()

	if err := os.Chdir(campaignRoot); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	ctx := context.Background()

	// Populate cache
	_, err = DetectCached(ctx)
	if err != nil {
		t.Fatalf("DetectCached() error = %v", err)
	}

	if !IsCached() {
		t.Error("IsCached() should be true after detection")
	}

	// Clear cache
	ClearCache()

	if IsCached() {
		t.Error("IsCached() should be false after ClearCache()")
	}
}

func TestCacheDisableEnvVar(t *testing.T) {
	// Create temp campaign
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "test-campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	ClearCache()

	if err := os.Chdir(campaignRoot); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	ctx := context.Background()

	// Set disable env var
	os.Setenv(EnvCacheDisable, "1")
	defer os.Unsetenv(EnvCacheDisable)

	// Detection should still work but not use cache
	root, err := DetectCached(ctx)
	if err != nil {
		t.Fatalf("DetectCached() error = %v", err)
	}
	if root != campaignRoot {
		t.Errorf("DetectCached() = %v, want %v", root, campaignRoot)
	}
}

func TestCacheConcurrency(t *testing.T) {
	// Create temp campaign
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "test-campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	ClearCache()

	if err := os.Chdir(campaignRoot); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	ctx := context.Background()

	// Run concurrent detections
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			root, err := DetectCached(ctx)
			if err != nil {
				errors <- err
				return
			}
			if root != campaignRoot {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent DetectCached() error: %v", err)
		}
	}
}

func TestWarmCache(t *testing.T) {
	// Create temp campaign
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "test-campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	ClearCache()

	if err := os.Chdir(campaignRoot); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	ctx := context.Background()

	// Cache should be empty
	if IsCached() {
		t.Error("IsCached() should be false before WarmCache")
	}

	// Warm the cache
	err = WarmCache(ctx)
	if err != nil {
		t.Fatalf("WarmCache() error = %v", err)
	}

	// Cache should be populated
	if !IsCached() {
		t.Error("IsCached() should be true after WarmCache")
	}
}

func TestCacheNotInCampaign(t *testing.T) {
	// Create temp directory that is NOT a campaign
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	ClearCache()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	ctx := context.Background()

	// Detection should fail and cache the error
	_, err = DetectCached(ctx)
	if err != ErrNotInCampaign {
		t.Errorf("DetectCached() error = %v, want %v", err, ErrNotInCampaign)
	}

	// Cache should still be populated (with error)
	if !IsCached() {
		t.Error("IsCached() should be true even after error")
	}

	// Second call should return cached error
	_, err = DetectCached(ctx)
	if err != ErrNotInCampaign {
		t.Errorf("DetectCached() second call error = %v, want %v", err, ErrNotInCampaign)
	}
}
