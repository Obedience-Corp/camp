package transfer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/git"
)

// ResolveCampaignPath resolves a "project:path" spec to an absolute path.
// If spec has no colon, it is treated as relative to cwd.
func ResolveCampaignPath(ctx context.Context, campRoot, spec string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	project, relPath, hasColon := parseSpec(spec)
	if !hasColon {
		// Plain path — resolve relative to cwd
		abs, err := filepath.Abs(spec)
		if err != nil {
			return "", fmt.Errorf("resolve path %q: %w", spec, err)
		}
		return abs, nil
	}

	// Resolve project name to path
	projectPath, err := resolveProject(ctx, campRoot, project)
	if err != nil {
		return "", err
	}

	resolved := filepath.Join(projectPath, relPath)
	return resolved, nil
}

// ValidatePathExists checks that the resolved path exists on disk.
func ValidatePathExists(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("path does not exist: %s", path)
	}
	return nil
}

// parseSpec splits "project:path" into its components.
// Returns (project, relPath, true) if colon found, or (spec, "", false) otherwise.
func parseSpec(spec string) (string, string, bool) {
	idx := strings.Index(spec, ":")
	if idx < 0 {
		return spec, "", false
	}
	return spec[:idx], spec[idx+1:], true
}

// resolveProject looks up a project name in the campaign submodule list.
func resolveProject(ctx context.Context, campRoot, name string) (string, error) {
	all, err := git.ListSubmodulePaths(ctx, campRoot)
	if err != nil {
		return "", fmt.Errorf("list submodules: %w", err)
	}

	nameLower := strings.ToLower(name)

	// Exact match on directory name
	for _, p := range all {
		if strings.ToLower(filepath.Base(p)) == nameLower {
			return filepath.Join(campRoot, p), nil
		}
	}

	// Prefix match
	var matches []string
	for _, p := range all {
		if strings.HasPrefix(strings.ToLower(filepath.Base(p)), nameLower) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 1 {
		return filepath.Join(campRoot, matches[0]), nil
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = filepath.Base(m)
		}
		return "", fmt.Errorf("ambiguous project %q: %s", name, strings.Join(names, ", "))
	}

	return "", fmt.Errorf("project %q not found", name)
}
