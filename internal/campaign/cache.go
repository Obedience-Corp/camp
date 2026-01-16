package campaign

import (
	"context"
	"os"
	"path/filepath"
	"sync"
)

// EnvCacheDisable is the environment variable to disable caching.
const EnvCacheDisable = "CAMP_CACHE_DISABLE"

// detectionCache holds the cached campaign root detection result.
type detectionCache struct {
	mu       sync.RWMutex
	cwd      string // cwd at time of detection
	root     string // detected campaign root
	detected bool   // whether detection has been performed
	err      error  // cached error (e.g., ErrNotInCampaign)
}

var cache = &detectionCache{}

// DetectCached returns campaign root, using cache if valid.
// The cache is invalidated when cwd changes.
func DetectCached(ctx context.Context) (string, error) {
	// Check if caching is disabled
	if os.Getenv(EnvCacheDisable) != "" {
		return DetectFromCwd(ctx)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Resolve symlinks for consistent comparison
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", err
	}

	// Try cache first (read lock)
	cache.mu.RLock()
	if cache.detected && cache.cwd == cwd {
		root := cache.root
		cachedErr := cache.err
		cache.mu.RUnlock()
		return root, cachedErr
	}
	cache.mu.RUnlock()

	// Cache miss - perform detection
	root, detectErr := DetectFromCwd(ctx)

	// Update cache (write lock)
	cache.mu.Lock()
	cache.cwd = cwd
	cache.root = root
	cache.err = detectErr
	cache.detected = true
	cache.mu.Unlock()

	return root, detectErr
}

// ClearCache invalidates the detection cache.
func ClearCache() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.detected = false
	cache.cwd = ""
	cache.root = ""
	cache.err = nil
}

// WarmCache proactively detects and caches campaign root.
// Returns any detection error.
func WarmCache(ctx context.Context) error {
	_, err := DetectCached(ctx)
	return err
}

// IsCached returns whether a valid cache entry exists for the current directory.
func IsCached() bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return false
	}

	cache.mu.RLock()
	defer cache.mu.RUnlock()
	return cache.detected && cache.cwd == cwd
}
