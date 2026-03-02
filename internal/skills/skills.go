// Package skills provides utilities for managing campaign skill symlinks.
package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
)

// SkillsSubdir is the subdirectory under .campaign/ that holds skills.
const SkillsSubdir = "skills"

// LinkState describes the state of a symlink at a given path.
type LinkState string

const (
	// StateValid means the path is a symlink pointing to the expected target.
	StateValid LinkState = "valid"
	// StateBroken means the path is a symlink but the target does not exist.
	StateBroken LinkState = "broken"
	// StateNotALink means the path exists but is not a symlink.
	StateNotALink LinkState = "not_a_link"
	// StateMissing means nothing exists at the path.
	StateMissing LinkState = "missing"
)

// ToolPaths maps tool names to their relative skill destination directories.
var ToolPaths = map[string]string{
	"claude": ".claude/skills",
	"agents": ".agents/skills",
}

// FindSkillsDir locates the .campaign/skills/ directory by detecting the
// campaign root and joining the skills subdirectory. Returns an error if
// not in a campaign or the skills directory does not exist.
func FindSkillsDir(ctx context.Context) (string, error) {
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return "", fmt.Errorf("find skills dir: %w", err)
	}

	skillsDir := filepath.Join(root, campaign.CampaignDir, SkillsSubdir)
	info, err := os.Stat(skillsDir)
	if err != nil {
		return "", fmt.Errorf("find skills dir: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("find skills dir: %s is not a directory", skillsDir)
	}

	return skillsDir, nil
}

// RelativeSymlinkTarget computes a relative path from the directory containing
// linkPath to targetPath. This produces portable symlinks that work regardless
// of the absolute campaign root location.
func RelativeSymlinkTarget(linkPath, targetPath string) (string, error) {
	linkDir := filepath.Dir(linkPath)
	rel, err := filepath.Rel(linkDir, targetPath)
	if err != nil {
		return "", fmt.Errorf("compute relative symlink target: %w", err)
	}
	return rel, nil
}

// CheckLinkState inspects the filesystem at path and returns its LinkState.
// If expectedTarget is non-empty and the path is a symlink, the resolved
// target is compared against expectedTarget to determine validity.
func CheckLinkState(path, expectedTarget string) (LinkState, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StateMissing, nil
		}
		return "", fmt.Errorf("check link state: %w", err)
	}

	// Not a symlink
	if info.Mode()&os.ModeSymlink == 0 {
		return StateNotALink, nil
	}

	// It's a symlink — resolve and compare
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// EvalSymlinks fails when the target doesn't exist
		return StateBroken, nil
	}

	if expectedTarget == "" {
		return StateValid, nil
	}

	// Resolve the expected target for comparison
	resolvedExpected, err := filepath.EvalSymlinks(expectedTarget)
	if err != nil {
		// Expected target doesn't exist on disk — if the link resolves to
		// something, compare raw paths as a fallback
		resolvedExpected = expectedTarget
	}

	if resolved == resolvedExpected {
		return StateValid, nil
	}

	return StateBroken, nil
}

// ResolveToolPath returns the relative destination directory for a given tool
// name. Returns an error if the tool is not recognized.
func ResolveToolPath(tool string) (string, error) {
	p, ok := ToolPaths[tool]
	if !ok {
		return "", fmt.Errorf("unknown tool %q: valid tools are: claude, agents", tool)
	}
	return p, nil
}

// PathType describes what exists at a given filesystem path.
type PathType string

const (
	TypeSymlink   PathType = "symlink"
	TypeDirectory PathType = "directory"
	TypeFile      PathType = "file"
	TypeMissing   PathType = "missing"
)

// CheckPathType determines what kind of filesystem entry exists at path.
func CheckPathType(path string) (PathType, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TypeMissing, nil
		}
		return "", fmt.Errorf("check path type: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return TypeSymlink, nil
	}
	if info.IsDir() {
		return TypeDirectory, nil
	}
	return TypeFile, nil
}
