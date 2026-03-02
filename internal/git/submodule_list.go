package git

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ListSubmodulePaths returns the filesystem paths of all submodules in the repo.
// Paths are relative to the repository root.
func ListSubmodulePaths(ctx context.Context, repoRoot string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"config", "-f", ".gitmodules", "--list")
	output, err := cmd.Output()
	if err != nil {
		// No .gitmodules means no submodules
		return nil, nil
	}

	var paths []string
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.Contains(line, ".path=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		paths = append(paths, parts[1])
	}

	return paths, nil
}

// ListSubmodulePathsFiltered returns submodule paths matching a prefix.
func ListSubmodulePathsFiltered(ctx context.Context, repoRoot, prefix string) ([]string, error) {
	all, err := ListSubmodulePaths(ctx, repoRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "list submodules")
	}

	if prefix == "" {
		return all, nil
	}

	var filtered []string
	for _, p := range all {
		if strings.HasPrefix(p, prefix) {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// ListSubmodulePathsRecursive returns submodule paths matching a prefix,
// including nested submodules one level deep within monorepos.
func ListSubmodulePathsRecursive(ctx context.Context, repoRoot, prefix string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	topLevel, err := ListSubmodulePathsFiltered(ctx, repoRoot, prefix)
	if err != nil {
		return nil, err
	}

	var result []string
	for _, p := range topLevel {
		result = append(result, p)

		// Check for nested submodules one level deep.
		nested, err := ListSubmodulePaths(ctx, filepath.Join(repoRoot, p))
		if err != nil || len(nested) == 0 {
			continue
		}
		for _, n := range nested {
			result = append(result, filepath.Join(p, n))
		}
	}

	return result, nil
}

// SubmoduleDisplayName returns a concise display name for a submodule path.
// For nested submodules (3+ path components like "projects/monorepo/child"),
// it returns "monorepo/child". For top-level submodules it returns the base name.
func SubmoduleDisplayName(relPath string) string {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	if len(parts) > 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return filepath.Base(relPath)
}
