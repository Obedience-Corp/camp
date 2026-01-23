package fest

import (
	"context"
	"testing"
)

func TestFindFestCLI(t *testing.T) {
	// Reset cache before testing
	ResetCache()

	path, err := FindFestCLI()
	if err != nil {
		// fest may not be installed in CI environments
		t.Skipf("fest CLI not found (skipping): %v", err)
	}

	if path == "" {
		t.Error("FindFestCLI() returned empty path")
	}

	t.Logf("fest found at: %s", path)
}

func TestIsFestAvailable(t *testing.T) {
	ResetCache()

	available := IsFestAvailable()
	t.Logf("fest available: %v", available)
}

func TestGetFestVersion(t *testing.T) {
	ResetCache()
	ctx := context.Background()

	// Skip if fest not installed
	path, err := FindFestCLI()
	if err != nil {
		t.Skip("fest not installed")
	}

	version, err := GetFestVersion(ctx, path)
	if err != nil {
		t.Fatalf("GetFestVersion() error = %v", err)
	}

	if version == "" {
		t.Error("Version is empty")
	}
	t.Logf("fest version: %s", version)
}

func TestVerifyFest(t *testing.T) {
	ResetCache()
	ctx := context.Background()

	info, err := VerifyFest(ctx)
	if err != nil {
		t.Skipf("fest not installed or not working: %v", err)
	}

	if info.Path == "" {
		t.Error("FestInfo.Path is empty")
	}
	if info.Version == "" {
		t.Error("FestInfo.Version is empty")
	}

	t.Logf("fest info: path=%s, version=%s", info.Path, info.Version)
}

func TestResetCache(t *testing.T) {
	// First find the CLI
	ResetCache()
	path1, _ := FindFestCLI()

	// Reset and find again
	ResetCache()
	path2, _ := FindFestCLI()

	// Should get same path (if found)
	if path1 != path2 {
		t.Errorf("Paths differ after reset: %q vs %q", path1, path2)
	}
}
