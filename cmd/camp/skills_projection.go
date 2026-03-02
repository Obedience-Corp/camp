package main

import (
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/skills"
)

// resolveSkillsDestination maps --tool or --path flags to an absolute destination.
func resolveSkillsDestination(root, tool, destPath string) (string, error) {
	if tool != "" {
		relPath, err := skills.ResolveToolPath(tool)
		if err != nil {
			return "", err
		}
		return filepath.Join(root, relPath), nil
	}
	if filepath.IsAbs(destPath) {
		return destPath, nil
	}
	return filepath.Join(root, destPath), nil
}
