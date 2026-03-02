package skills

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ProjectionSummary tracks the results of projecting skill bundles.
type ProjectionSummary struct {
	Created       int
	Replaced      int
	AlreadyLinked int
	Conflicts     int
	ConflictNames []string
}

// ProjectionState describes the current projection state for a tool destination.
type ProjectionState struct {
	TotalSkills int
	Linked      int
	Broken      int
	Mismatched  int
	Conflicts   int
}

// IsManagedSkillEntryLink checks whether the symlink at linkPath was created
// by camp skills projection (points into skillsDir or matches expectedTarget).
func IsManagedSkillEntryLink(linkPath, expectedTarget, skillsDir string) (bool, error) {
	info, err := os.Lstat(linkPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, camperrors.Wrap(err, "inspect skill link")
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}

	raw, err := os.Readlink(linkPath)
	if err != nil {
		return false, camperrors.Wrap(err, "read skill link")
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

	// Fallback for broken managed links whose raw target still points into
	// the canonical skills directory.
	return isWithinOrEqual(cleanSkills, abs) || (resolvedSkillsErr == nil && isWithinOrEqual(resolvedSkills, abs)), nil
}

// resolveSymlinkTargetAbs resolves a possibly-relative symlink target to an
// absolute path based on the directory containing the link.
func resolveSymlinkTargetAbs(linkPath, rawTarget string) string {
	if filepath.IsAbs(rawTarget) {
		return filepath.Clean(rawTarget)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(linkPath), rawTarget))
}

// isWithinOrEqual returns true if target is equal to or a proper subpath of root.
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

// EnsureProjectionDirectory ensures destDir exists as a directory suitable for
// skill projection. Returns an error if the path is a file or symlink.
func EnsureProjectionDirectory(destDir string, dryRun bool, errOut io.Writer) error {
	pathType, err := CheckPathType(destDir)
	if err != nil {
		return err
	}

	switch pathType {
	case TypeMissing:
		if dryRun {
			fmt.Fprintf(errOut, "would create destination directory: %s\n", destDir)
			return nil
		}
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return camperrors.Wrap(err, "create destination directory")
		}
		return nil

	case TypeDirectory:
		return nil

	case TypeFile:
		return fmt.Errorf("destination exists and is a file: %s", destDir)

	case TypeSymlink:
		return fmt.Errorf("destination exists as a symlink; camp skills now projects individual skill bundles into a directory: %s", destDir)
	}

	return fmt.Errorf("unsupported destination type for %s", destDir)
}

// CreateSkillProjectionLink creates a relative symlink from linkPath to sourcePath.
func CreateSkillProjectionLink(linkPath, sourcePath string, dryRun bool) error {
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return camperrors.Wrap(err, "create parent directories")
	}
	relTarget, err := RelativeSymlinkTarget(linkPath, sourcePath)
	if err != nil {
		return err
	}
	if err := os.Symlink(relTarget, linkPath); err != nil {
		return camperrors.Wrap(err, "create symlink")
	}
	return nil
}

// ProjectSkillEntries projects skill bundles from skillsDir into destDir.
func ProjectSkillEntries(destDir, skillsDir string, slugs []string, dryRun, force bool) (ProjectionSummary, error) {
	var summary ProjectionSummary

	for _, slug := range slugs {
		sourcePath := filepath.Join(skillsDir, slug)
		destPath := filepath.Join(destDir, slug)

		state, err := CheckLinkState(destPath, sourcePath)
		if err != nil {
			return summary, camperrors.Wrapf(err, "check skill entry %q", slug)
		}

		switch state {
		case StateValid:
			summary.AlreadyLinked++

		case StateMissing:
			if err := CreateSkillProjectionLink(destPath, sourcePath, dryRun); err != nil {
				return summary, camperrors.Wrapf(err, "link skill %q", slug)
			}
			summary.Created++

		case StateNotALink:
			addConflict(&summary, slug)

		case StateBroken, StateMismatch:
			managed, err := IsManagedSkillEntryLink(destPath, sourcePath, skillsDir)
			if err != nil {
				return summary, err
			}
			if !managed && !force {
				addConflict(&summary, slug)
				continue
			}
			if !dryRun {
				if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
					return summary, camperrors.Wrapf(err, "remove broken skill link %q", slug)
				}
			}
			if err := CreateSkillProjectionLink(destPath, sourcePath, dryRun); err != nil {
				return summary, camperrors.Wrapf(err, "relink skill %q", slug)
			}
			summary.Replaced++
		}
	}

	return summary, nil
}

func addConflict(summary *ProjectionSummary, name string) {
	summary.Conflicts++
	if len(summary.ConflictNames) < 5 {
		summary.ConflictNames = append(summary.ConflictNames, name)
	}
}

// InspectSkillProjection returns the current projection state for a destination.
func InspectSkillProjection(destDir, skillsDir string, slugs []string) (ProjectionState, error) {
	state := ProjectionState{TotalSkills: len(slugs)}
	for _, slug := range slugs {
		sourcePath := filepath.Join(skillsDir, slug)
		destPath := filepath.Join(destDir, slug)

		linkState, err := CheckLinkState(destPath, sourcePath)
		if err != nil {
			return state, camperrors.Wrapf(err, "check skill entry %q", slug)
		}

		switch linkState {
		case StateValid:
			state.Linked++
		case StateNotALink:
			state.Conflicts++
		case StateMismatch:
			managed, err := IsManagedSkillEntryLink(destPath, sourcePath, skillsDir)
			if err != nil {
				return state, err
			}
			if managed {
				state.Mismatched++
			} else {
				state.Conflicts++
			}
		case StateBroken:
			managed, err := IsManagedSkillEntryLink(destPath, sourcePath, skillsDir)
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

// RemoveProjectedSkillEntries removes managed skill projection symlinks from destDir.
func RemoveProjectedSkillEntries(destDir, skillsDir string, slugs []string, dryRun bool) (int, error) {
	removed := 0
	for _, slug := range slugs {
		sourcePath := filepath.Join(skillsDir, slug)
		destPath := filepath.Join(destDir, slug)

		linkState, err := CheckLinkState(destPath, sourcePath)
		if err != nil {
			return removed, camperrors.Wrapf(err, "check skill entry %q", slug)
		}

		shouldRemove := false
		switch linkState {
		case StateValid:
			shouldRemove = true
		case StateBroken, StateMismatch:
			managed, err := IsManagedSkillEntryLink(destPath, sourcePath, skillsDir)
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
				return removed, camperrors.Wrapf(err, "remove skill link %q", slug)
			}
		}
		removed++
	}
	return removed, nil
}
