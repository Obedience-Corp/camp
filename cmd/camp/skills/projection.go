package skills

import (
	"path/filepath"

	intskills "github.com/Obedience-Corp/camp/internal/skills"
)

// resolveSkillsDestination maps --tool or --path flags to an absolute destination.
func resolveSkillsDestination(root, tool, destPath string) (string, error) {
	if tool != "" {
		relPath, err := intskills.ResolveToolPath(tool)
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
