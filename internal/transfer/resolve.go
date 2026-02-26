package transfer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
)

// ResolveCampaignRelative resolves a path relative to the campaign root.
// Absolute paths are returned cleaned. Relative paths are joined with campRoot.
func ResolveCampaignRelative(campRoot, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(campRoot, path)
}

// ResolveCrossCampaignPath resolves a "campaign:path" spec to an absolute path.
// If spec has no colon, the path is resolved relative to the current campaign root.
// If spec has a colon, the prefix is looked up in the campaign registry.
func ResolveCrossCampaignPath(ctx context.Context, currentCampRoot, spec string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	campaign, relPath, hasColon := parseSpec(spec)
	if !hasColon {
		// No campaign prefix — resolve relative to current campaign
		return ResolveCampaignRelative(currentCampRoot, spec), nil
	}

	// Look up campaign in registry
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return "", fmt.Errorf("load campaign registry: %w", err)
	}

	entry, ok := reg.Get(campaign)
	if !ok {
		return "", fmt.Errorf("campaign %q not found in registry", campaign)
	}

	resolved := filepath.Join(entry.Path, relPath)
	return resolved, nil
}

// ValidatePathExists checks that the resolved path exists on disk.
func ValidatePathExists(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("path does not exist: %s", path)
	}
	return nil
}

// parseSpec splits "prefix:path" into its components.
// Returns (prefix, relPath, true) if colon found, or (spec, "", false) otherwise.
func parseSpec(spec string) (string, string, bool) {
	idx := strings.Index(spec, ":")
	if idx < 0 {
		return spec, "", false
	}
	return spec[:idx], spec[idx+1:], true
}

// IsDestDir returns true if the path exists and is a directory, or ends with a path separator.
func IsDestDir(path string) bool {
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, string(filepath.Separator)) {
		return true
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
