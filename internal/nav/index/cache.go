package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/obediencecorp/camp/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	// cacheDir is the directory under campaign root where cache files are stored.
	cacheDir = ".campaign/cache"
	// cacheFile is the name of the navigation index cache file.
	cacheFile = "nav-index.yaml"
	// cacheMaxAge is the maximum age before cache is considered stale.
	cacheMaxAge = 24 * time.Hour
)

// CachePath returns the cache file path for a campaign root.
func CachePath(campaignRoot string) string {
	return filepath.Join(campaignRoot, cacheDir, cacheFile)
}

// Save writes the index to the cache file.
// The write is atomic (write to temp file, then rename) to prevent corruption.
func Save(idx *Index, campaignRoot string) error {
	if idx == nil {
		return fmt.Errorf("cannot save nil index")
	}

	path := CachePath(campaignRoot)

	// Ensure cache directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(idx)
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	// Write atomically (write to temp, rename)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Clean up temp file
		return fmt.Errorf("failed to rename cache file: %w", err)
	}

	return nil
}

// Load reads the index from the cache file.
// Returns nil, nil if the cache file doesn't exist.
func Load(campaignRoot string) (*Index, error) {
	path := CachePath(campaignRoot)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil // Cache doesn't exist
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var idx Index
	if err := yaml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("failed to parse cache file: %w", err)
	}

	return &idx, nil
}

// IsFresh checks if the index is within the maximum age.
func IsFresh(idx *Index) bool {
	if idx == nil {
		return false
	}

	// Check age
	if time.Since(idx.BuildTime) > cacheMaxAge {
		return false
	}

	return true
}

// IsStale checks if the cache should be rebuilt.
// Returns true if the cache is nil, too old, or if project configuration changed.
func IsStale(idx *Index, campaignRoot string) bool {
	if !IsFresh(idx) {
		return true
	}

	// Check if .gitmodules changed (projects added/removed via submodules)
	gitmodules := filepath.Join(campaignRoot, ".gitmodules")
	info, err := os.Stat(gitmodules)
	if err == nil && info.ModTime().After(idx.BuildTime) {
		return true
	}

	// Check if campaign.yaml changed
	campaignYaml := filepath.Join(campaignRoot, ".campaign", "campaign.yaml")
	info, err = os.Stat(campaignYaml)
	if err == nil && info.ModTime().After(idx.BuildTime) {
		return true
	}

	return false
}

// GetOrBuild loads from cache or builds fresh.
// If forceRebuild is true, always rebuilds regardless of cache state.
func GetOrBuild(ctx context.Context, campaignRoot string, forceRebuild bool) (*Index, error) {
	// Check context before starting
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if !forceRebuild {
		idx, err := Load(campaignRoot)
		if err != nil {
			// Log warning but continue to rebuild
			// In production, this would go to a logger
		}
		if idx != nil && !IsStale(idx, campaignRoot) {
			return idx, nil
		}
	}

	// Load campaign config to get project shortcuts
	var projects []config.ProjectConfig
	if cfg, err := config.LoadCampaignConfig(ctx, campaignRoot); err == nil {
		projects = cfg.Projects
	}

	// Build fresh
	builder := NewBuilder(campaignRoot).WithProjects(projects)
	idx, err := builder.Build(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build index: %w", err)
	}

	// Save to cache (don't fail if save fails)
	if saveErr := Save(idx, campaignRoot); saveErr != nil {
		// In production, this would be logged as a warning
	}

	return idx, nil
}

// Delete removes the cache file.
func Delete(campaignRoot string) error {
	path := CachePath(campaignRoot)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil // Already gone
	}
	return err
}

// CacheInfo contains metadata about the cache file.
type CacheInfo struct {
	// Path is the absolute path to the cache file.
	Path string
	// Exists indicates if the cache file exists.
	Exists bool
	// ModTime is when the cache file was last modified.
	ModTime time.Time
	// Size is the cache file size in bytes.
	Size int64
	// Age is how old the cache is.
	Age time.Duration
	// Fresh indicates if the cache is within max age.
	Fresh bool
}

// Info returns metadata about the cache file.
func Info(campaignRoot string) (*CacheInfo, error) {
	path := CachePath(campaignRoot)

	info := &CacheInfo{
		Path:   path,
		Exists: false,
	}

	stat, err := os.Stat(path)
	if os.IsNotExist(err) {
		return info, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat cache file: %w", err)
	}

	info.Exists = true
	info.ModTime = stat.ModTime()
	info.Size = stat.Size()
	info.Age = time.Since(stat.ModTime())
	info.Fresh = info.Age < cacheMaxAge

	return info, nil
}
