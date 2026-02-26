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

// ResolveAtPrefix expands @ prefix shortcuts using campaign shortcuts configuration.
// For example, "@p/fest" with shortcuts {"p": "projects/"} resolves to "<campRoot>/projects/fest".
// Paths without @ prefix are returned unchanged with no error.
// Returns the resolved absolute path, or an error if the @ shortcut is unknown.
func ResolveAtPrefix(campRoot, path string, shortcuts map[string]string) (string, error) {
	if !strings.HasPrefix(path, "@") {
		return path, nil
	}

	raw := path[1:] // strip leading @
	key := raw
	rest := ""
	if idx := strings.IndexByte(raw, '/'); idx != -1 {
		key = raw[:idx]
		rest = raw[idx+1:]
	}

	dir, ok := shortcuts[key]
	if !ok {
		var keys []string
		for k := range shortcuts {
			keys = append(keys, "@"+k)
		}
		return "", fmt.Errorf("unknown shortcut: @%s (valid: %s)", key, strings.Join(keys, ", "))
	}

	resolved := filepath.Join(campRoot, dir)
	if rest != "" {
		resolved = filepath.Join(resolved, rest)
	}
	return resolved, nil
}

// ResolveCwdRelative resolves a path relative to the current working directory.
// Absolute paths are returned cleaned. Relative paths are joined with cwd.
func ResolveCwdRelative(path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return filepath.Join(cwd, path), nil
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
