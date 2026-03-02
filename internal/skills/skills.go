// Package skills provides utilities for managing campaign skill symlinks.
package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	// StateMismatch means the path is a symlink that resolves to a valid path
	// but does not match the expected target.
	StateMismatch LinkState = "mismatch"
	// StateNotALink means the path exists but is not a symlink.
	StateNotALink LinkState = "not_a_link"
	// StateMissing means nothing exists at the path.
	StateMissing LinkState = "missing"
)

// toolPaths maps tool names to their relative skill destination directories.
var toolPaths = map[string]string{
	"claude": ".claude/skills",
	"agents": ".agents/skills",
}

// ToolPaths returns a copy of the tool-to-path mapping. Callers cannot mutate
// the internal registry.
func ToolPaths() map[string]string {
	cp := make(map[string]string, len(toolPaths))
	for k, v := range toolPaths {
		cp[k] = v
	}
	return cp
}

// ToolNames returns sorted tool names from the registry.
func ToolNames() []string {
	names := make([]string, 0, len(toolPaths))
	for k := range toolPaths {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
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
		if os.IsNotExist(err) {
			// Broken symlink: target missing
			return StateBroken, nil
		}
		return "", fmt.Errorf("check link state: resolve symlink: %w", err)
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

	return StateMismatch, nil
}

// DiscoverSkillSlugs returns canonical skill bundle directory names found under
// skillsDir. A valid bundle is a directory (or symlink to a directory)
// containing a SKILL.md file.
func DiscoverSkillSlugs(skillsDir string) ([]string, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, fmt.Errorf("discover skill slugs: %w", err)
	}

	slugs := make([]string, 0, len(entries))
	for _, entry := range entries {
		entryPath := filepath.Join(skillsDir, entry.Name())

		isDir := entry.IsDir()
		if !isDir && entry.Type()&os.ModeSymlink != 0 {
			info, statErr := os.Stat(entryPath)
			if statErr != nil || !info.IsDir() {
				continue
			}
			isDir = true
		}
		if !isDir {
			continue
		}

		skillFile := filepath.Join(entryPath, "SKILL.md")
		info, statErr := os.Stat(skillFile)
		if statErr != nil || info.IsDir() {
			continue
		}
		slugs = append(slugs, entry.Name())
	}

	sort.Strings(slugs)
	return slugs, nil
}

// ResolveToolPath returns the relative destination directory for a given tool
// name. Returns an error if the tool is not recognized.
func ResolveToolPath(tool string) (string, error) {
	p, ok := toolPaths[tool]
	if !ok {
		return "", fmt.Errorf("unknown tool %q: valid tools are: %s", tool, strings.Join(ToolNames(), ", "))
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

// ValidateDestination checks whether dest is a safe symlink destination
// relative to the campaign root. It resolves symlinks in existing parent
// directories before checking boundaries to prevent out-of-root escapes.
//
// It rejects paths that could cause catastrophic data loss if force-removed:
// campaign root, .campaign/, filesystem root, or paths outside the campaign.
func ValidateDestination(dest, campaignRoot string) error {
	if strings.TrimSpace(dest) == "" {
		return fmt.Errorf("destination path cannot be empty")
	}
	if strings.TrimSpace(campaignRoot) == "" {
		return fmt.Errorf("campaign root cannot be empty")
	}

	// Resolve both paths through existing parent symlinks. This ensures a path
	// that lexically appears in-root cannot escape via a symlinked parent.
	resolvedRoot, err := resolvePathWithExistingParent(campaignRoot)
	if err != nil {
		return fmt.Errorf("resolve campaign root: %w", err)
	}
	resolvedDest, err := resolvePathWithExistingParent(dest)
	if err != nil {
		return fmt.Errorf("resolve destination: %w", err)
	}

	// Reject filesystem root
	if resolvedDest == string(filepath.Separator) {
		return fmt.Errorf("refusing to use filesystem root as destination")
	}

	// Reject campaign root itself
	if resolvedDest == resolvedRoot {
		return fmt.Errorf("refusing to use campaign root as destination: %s", resolvedDest)
	}

	// Reject .campaign/ directory
	campaignDir := filepath.Join(resolvedRoot, campaign.CampaignDir)
	if resolvedDest == campaignDir || isSubpath(resolvedDest, campaignDir) {
		return fmt.Errorf("refusing to use .campaign/ directory as destination: %s", resolvedDest)
	}

	// Reject paths outside the campaign root
	if !isSubpath(resolvedDest, resolvedRoot) {
		return fmt.Errorf("destination must be inside campaign root: %s", resolvedDest)
	}

	return nil
}

// resolvePathWithExistingParent resolves symlinks in the deepest existing
// ancestor of path, then rejoins any non-existent suffix.
//
// Example:
//
//	/campaign/escape/custom where "escape" is a symlink to /outside
//
// resolves to:
//
//	/outside/custom
func resolvePathWithExistingParent(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	absPath = filepath.Clean(absPath)

	existing := absPath
	missing := make([]string, 0, 4)

	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %s: %w", existing, err)
		}

		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}

	resolvedExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks for %s: %w", existing, err)
	}

	resolved := resolvedExisting
	for _, part := range missing {
		resolved = filepath.Join(resolved, part)
	}

	return filepath.Clean(resolved), nil
}

// isSubpath returns true if child is a proper subpath of parent.
func isSubpath(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." || filepath.IsAbs(rel) {
		return false
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

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
