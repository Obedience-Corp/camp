package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/skills"
)

type skillProjectionSummary struct {
	Created       int
	Replaced      int
	AlreadyLinked int
	Conflicts     int
	ConflictNames []string
}

type skillProjectionState struct {
	TotalSkills int
	Linked      int
	Broken      int
	Conflicts   int
}

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

func resolveSymlinkTargetAbs(linkPath, rawTarget string) string {
	if filepath.IsAbs(rawTarget) {
		return filepath.Clean(rawTarget)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(linkPath), rawTarget))
}

func isWithinOrEqual(root, target string) bool {
	cleanRoot := filepath.Clean(root)
	cleanTarget := filepath.Clean(target)
	rel, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func isManagedSkillEntryLink(linkPath, expectedTarget, skillsDir string) (bool, error) {
	info, err := os.Lstat(linkPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect skill link: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}

	raw, err := os.Readlink(linkPath)
	if err != nil {
		return false, fmt.Errorf("read skill link: %w", err)
	}
	abs := resolveSymlinkTargetAbs(linkPath, raw)

	cleanExpected := filepath.Clean(expectedTarget)
	resolvedExpected, resolvedExpectedErr := filepath.EvalSymlinks(expectedTarget)
	cleanSkills := filepath.Clean(skillsDir)
	resolvedSkills, resolvedSkillsErr := filepath.EvalSymlinks(skillsDir)

	if abs == cleanExpected || isWithinOrEqual(cleanSkills, abs) {
		return true, nil
	}
	if resolvedExpectedErr == nil && abs == resolvedExpected {
		return true, nil
	}
	if resolvedSkillsErr == nil && isWithinOrEqual(resolvedSkills, abs) {
		return true, nil
	}

	resolvedAbs, resolveAbsErr := filepath.EvalSymlinks(abs)
	if resolveAbsErr == nil {
		if resolvedAbs == cleanExpected || isWithinOrEqual(cleanSkills, resolvedAbs) {
			return true, nil
		}
		if resolvedExpectedErr == nil && resolvedAbs == resolvedExpected {
			return true, nil
		}
		if resolvedSkillsErr == nil && isWithinOrEqual(resolvedSkills, resolvedAbs) {
			return true, nil
		}
	}

	// Fallback for broken managed links whose raw target still points under
	// .campaign/skills.
	return strings.Contains(filepath.ToSlash(abs), "/.campaign/skills/"), nil
}

func ensureProjectionDirectory(destDir string, dryRun bool, errOut io.Writer) error {
	pathType, err := skills.CheckPathType(destDir)
	if err != nil {
		return err
	}

	switch pathType {
	case skills.TypeMissing:
		if dryRun {
			fmt.Fprintf(errOut, "would create destination directory: %s\n", destDir)
			return nil
		}
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return fmt.Errorf("create destination directory: %w", err)
		}
		return nil

	case skills.TypeDirectory:
		return nil

	case skills.TypeFile:
		return fmt.Errorf("destination exists and is a file: %s", destDir)

	case skills.TypeSymlink:
		return fmt.Errorf("destination exists as a symlink; camp skills now projects individual skill bundles into a directory: %s", destDir)
	}

	return fmt.Errorf("unsupported destination type for %s", destDir)
}

func createSkillProjectionLink(linkPath, sourcePath string, dryRun bool) error {
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return fmt.Errorf("create parent directories: %w", err)
	}
	relTarget, err := skills.RelativeSymlinkTarget(linkPath, sourcePath)
	if err != nil {
		return err
	}
	if err := os.Symlink(relTarget, linkPath); err != nil {
		return fmt.Errorf("create symlink: %w", err)
	}
	return nil
}

func addConflict(summary *skillProjectionSummary, name string) {
	summary.Conflicts++
	if len(summary.ConflictNames) < 5 {
		summary.ConflictNames = append(summary.ConflictNames, name)
	}
}

func projectSkillEntries(destDir, skillsDir string, slugs []string, dryRun, force bool) (skillProjectionSummary, error) {
	var summary skillProjectionSummary

	for _, slug := range slugs {
		sourcePath := filepath.Join(skillsDir, slug)
		destPath := filepath.Join(destDir, slug)

		state, err := skills.CheckLinkState(destPath, sourcePath)
		if err != nil {
			return summary, fmt.Errorf("check skill entry %q: %w", slug, err)
		}

		switch state {
		case skills.StateValid:
			summary.AlreadyLinked++

		case skills.StateMissing:
			if err := createSkillProjectionLink(destPath, sourcePath, dryRun); err != nil {
				return summary, fmt.Errorf("link skill %q: %w", slug, err)
			}
			summary.Created++

		case skills.StateNotALink:
			addConflict(&summary, slug)

		case skills.StateBroken:
			managed, err := isManagedSkillEntryLink(destPath, sourcePath, skillsDir)
			if err != nil {
				return summary, err
			}
			if !managed && !force {
				addConflict(&summary, slug)
				continue
			}
			if !dryRun {
				if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
					return summary, fmt.Errorf("remove broken skill link %q: %w", slug, err)
				}
			}
			if err := createSkillProjectionLink(destPath, sourcePath, dryRun); err != nil {
				return summary, fmt.Errorf("relink skill %q: %w", slug, err)
			}
			summary.Replaced++
		}
	}

	return summary, nil
}

func inspectSkillProjection(destDir, skillsDir string, slugs []string) (skillProjectionState, error) {
	state := skillProjectionState{TotalSkills: len(slugs)}
	for _, slug := range slugs {
		sourcePath := filepath.Join(skillsDir, slug)
		destPath := filepath.Join(destDir, slug)

		linkState, err := skills.CheckLinkState(destPath, sourcePath)
		if err != nil {
			return state, fmt.Errorf("check skill entry %q: %w", slug, err)
		}

		switch linkState {
		case skills.StateValid:
			state.Linked++
		case skills.StateNotALink:
			state.Conflicts++
		case skills.StateBroken:
			managed, err := isManagedSkillEntryLink(destPath, sourcePath, skillsDir)
			if err != nil {
				return state, err
			}
			if managed {
				state.Broken++
			} else {
				state.Conflicts++
			}
		}
	}
	return state, nil
}

func removeProjectedSkillEntries(destDir, skillsDir string, slugs []string, dryRun bool) (int, error) {
	removed := 0
	for _, slug := range slugs {
		sourcePath := filepath.Join(skillsDir, slug)
		destPath := filepath.Join(destDir, slug)

		linkState, err := skills.CheckLinkState(destPath, sourcePath)
		if err != nil {
			return removed, fmt.Errorf("check skill entry %q: %w", slug, err)
		}

		shouldRemove := false
		switch linkState {
		case skills.StateValid:
			shouldRemove = true
		case skills.StateBroken:
			managed, err := isManagedSkillEntryLink(destPath, sourcePath, skillsDir)
			if err != nil {
				return removed, err
			}
			shouldRemove = managed
		}

		if !shouldRemove {
			continue
		}
		if !dryRun {
			if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
				return removed, fmt.Errorf("remove skill link %q: %w", slug, err)
			}
		}
		removed++
	}
	return removed, nil
}
