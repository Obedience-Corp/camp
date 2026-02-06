package transfer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/git"
)

// ResolveShortcut resolves a move shortcut into full source and destination paths.
// Given a file path within the current project and a target project name,
// it computes the same relative path in the target project.
func ResolveShortcut(ctx context.Context, campRoot, cwd, file, targetProject string) (src, dest string, err error) {
	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}

	// Find which project cwd is in
	currentProject, projectPath, err := detectCurrentProject(ctx, campRoot, cwd)
	if err != nil {
		return "", "", err
	}

	// Compute relative path of file within the current project
	absFile := file
	if !filepath.IsAbs(file) {
		absFile = filepath.Join(cwd, file)
	}
	absFile = filepath.Clean(absFile)

	relPath, err := filepath.Rel(projectPath, absFile)
	if err != nil {
		return "", "", fmt.Errorf("compute relative path: %w", err)
	}
	if strings.HasPrefix(relPath, "..") {
		return "", "", fmt.Errorf("file %q is outside project %q", file, currentProject)
	}

	// Resolve source to absolute path
	src = absFile

	// Resolve destination using project:path syntax
	destSpec := targetProject + ":" + relPath
	dest, err = ResolveCampaignPath(ctx, campRoot, destSpec)
	if err != nil {
		return "", "", fmt.Errorf("resolve destination: %w", err)
	}

	return src, dest, nil
}

// detectCurrentProject determines which submodule the given directory is inside.
func detectCurrentProject(ctx context.Context, campRoot, dir string) (string, string, error) {
	all, err := git.ListSubmodulePaths(ctx, campRoot)
	if err != nil {
		return "", "", fmt.Errorf("list submodules: %w", err)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", "", fmt.Errorf("resolve directory: %w", err)
	}

	// Find the longest matching submodule path (most specific match)
	var bestName, bestPath string
	for _, p := range all {
		absSubmodule := filepath.Join(campRoot, p)
		if strings.HasPrefix(absDir, absSubmodule+"/") || absDir == absSubmodule {
			if len(absSubmodule) > len(bestPath) {
				bestName = filepath.Base(p)
				bestPath = absSubmodule
			}
		}
	}

	if bestPath == "" {
		return "", "", fmt.Errorf("current directory is not inside a known project")
	}
	return bestName, bestPath, nil
}
