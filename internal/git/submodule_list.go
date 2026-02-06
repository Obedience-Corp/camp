package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
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
		return nil, fmt.Errorf("list submodules: %w", err)
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
