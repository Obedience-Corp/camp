package intent

import (
	"bytes"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intentaudit "github.com/Obedience-Corp/camp/internal/intent/audit"
)

func moveIntentAuditFile(legacyRoot, canonicalRoot string) error {
	srcPath := intentaudit.FilePath(legacyRoot)
	if _, err := os.Stat(srcPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "stat %s", srcPath)
	}

	dstPath := intentaudit.FilePath(canonicalRoot)
	if info, err := os.Stat(dstPath); err == nil {
		if info.Size() > 0 {
			return camperrors.Wrapf(ErrIntentMigrationConflict, "audit log already exists at %s", dstPath)
		}
		if err := os.Remove(dstPath); err != nil {
			return camperrors.Wrapf(err, "removing empty audit log %s", dstPath)
		}
	} else if !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "stat %s", dstPath)
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return camperrors.Wrapf(err, "creating directory %s", filepath.Dir(dstPath))
	}

	if err := os.Rename(srcPath, dstPath); err != nil {
		return camperrors.Wrapf(err, "moving audit log %s", srcPath)
	}

	return nil
}

func collectIntentAuditMove(legacyRoot, canonicalRoot string, moves *[]PlannedPathMove) error {
	srcPath := intentaudit.FilePath(legacyRoot)
	if _, err := os.Stat(srcPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "stat %s", srcPath)
	}

	dstPath := intentaudit.FilePath(canonicalRoot)
	if info, err := os.Stat(dstPath); err == nil {
		if info.Size() > 0 {
			return camperrors.Wrapf(ErrIntentMigrationConflict, "audit log already exists at %s", dstPath)
		}
	} else if !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "stat %s", dstPath)
	}

	*moves = append(*moves, PlannedPathMove{Source: srcPath, Dest: dstPath})
	return nil
}

func moveIntentMarkerFile(legacyRoot, canonicalRoot string) error {
	srcPath := intentMarkerPath(legacyRoot)
	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "reading %s", srcPath)
	}

	dstPath := intentMarkerPath(canonicalRoot)
	if dstData, err := os.ReadFile(dstPath); err == nil {
		if bytes.Equal(dstData, srcData) {
			if err := os.Remove(srcPath); err != nil {
				return camperrors.Wrapf(err, "removing %s", srcPath)
			}
			return nil
		}
	} else if !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "reading %s", dstPath)
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return camperrors.Wrapf(err, "creating directory %s", filepath.Dir(dstPath))
	}
	if err := os.WriteFile(dstPath, srcData, 0644); err != nil {
		return camperrors.Wrapf(err, "writing %s", dstPath)
	}
	if err := os.Remove(srcPath); err != nil {
		return camperrors.Wrapf(err, "removing %s", srcPath)
	}

	return nil
}

func collectIntentMarkerMove(legacyRoot, canonicalRoot string, moves *[]PlannedPathMove) error {
	srcPath := intentMarkerPath(legacyRoot)
	if _, err := os.Stat(srcPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "stat %s", srcPath)
	}

	*moves = append(*moves, PlannedPathMove{
		Source: srcPath,
		Dest:   intentMarkerPath(canonicalRoot),
	})
	return nil
}

func collectIntentTreeMoves(srcDir, dstDir string, moves *[]PlannedPathMove) error {
	if _, err := os.Stat(srcDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "stat %s", srcDir)
	}

	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		*moves = append(*moves, PlannedPathMove{Source: srcDir, Dest: dstDir})
		return nil
	} else if err != nil {
		return camperrors.Wrapf(err, "stat %s", dstDir)
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return camperrors.Wrapf(err, "reading directory %s", srcDir)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		if entry.IsDir() {
			if err := collectIntentTreeMoves(srcPath, dstPath, moves); err != nil {
				return err
			}
			continue
		}

		if _, err := os.Stat(dstPath); err == nil {
			if isIntentScaffoldBasename(entry.Name()) {
				continue
			}
			return camperrors.Wrapf(ErrIntentMigrationConflict, "destination already exists for %s", dstPath)
		} else if !os.IsNotExist(err) {
			return camperrors.Wrapf(err, "stat %s", dstPath)
		}

		*moves = append(*moves, PlannedPathMove{Source: srcPath, Dest: dstPath})
	}

	return nil
}

func collectLegacyIntentScaffoldFiles(legacyRoot string) ([]string, error) {
	var cleanup []string
	for _, relPath := range legacyIntentScaffoldFiles {
		absPath := filepath.Join(legacyRoot, relPath)
		if _, err := os.Stat(absPath); err == nil {
			cleanup = append(cleanup, absPath)
			continue
		} else if !os.IsNotExist(err) {
			return nil, camperrors.Wrapf(err, "stat %s", absPath)
		}
	}
	return cleanup, nil
}

func cleanupLegacyIntentScaffold(legacyRoot string) error {
	cleanupFiles, err := collectLegacyIntentScaffoldFiles(legacyRoot)
	if err != nil {
		return err
	}

	for _, path := range cleanupFiles {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return camperrors.Wrapf(err, "removing %s", path)
		}
	}

	for _, relDir := range []string{
		filepath.Join("dungeon", string(StatusDone)),
		filepath.Join("dungeon", string(StatusKilled)),
		filepath.Join("dungeon", string(StatusArchived)),
		filepath.Join("dungeon", string(StatusSomeday)),
		"dungeon",
		string(StatusInbox),
		string(StatusReady),
		string(StatusActive),
		"",
	} {
		dir := legacyRoot
		if relDir != "" {
			dir = filepath.Join(legacyRoot, relDir)
		}
		if err := removeDirIfEmpty(dir); err != nil {
			return err
		}
	}

	return nil
}

func removeDirIfEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "reading directory %s", dir)
	}
	if len(entries) != 0 {
		return nil
	}
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "removing %s", dir)
	}
	return nil
}

func moveIntentTree(srcDir, dstDir string) error {
	if _, err := os.Stat(srcDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "stat %s", srcDir)
	}

	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(dstDir), 0755); err != nil {
			return camperrors.Wrapf(err, "creating directory %s", filepath.Dir(dstDir))
		}
		if err := os.Rename(srcDir, dstDir); err != nil {
			return camperrors.Wrapf(err, "moving %s", srcDir)
		}
		return nil
	} else if err != nil {
		return camperrors.Wrapf(err, "stat %s", dstDir)
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return camperrors.Wrapf(err, "reading directory %s", srcDir)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		if entry.IsDir() {
			if err := moveIntentTree(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}

		if _, err := os.Stat(dstPath); err == nil {
			if isIntentScaffoldBasename(entry.Name()) {
				if err := os.Remove(srcPath); err != nil {
					return camperrors.Wrapf(err, "removing %s", srcPath)
				}
				continue
			}
			return camperrors.Wrapf(ErrIntentMigrationConflict, "destination already exists for %s", dstPath)
		} else if !os.IsNotExist(err) {
			return camperrors.Wrapf(err, "stat %s", dstPath)
		}

		if err := os.Rename(srcPath, dstPath); err != nil {
			return camperrors.Wrapf(err, "moving %s", srcPath)
		}
	}

	if remaining, err := os.ReadDir(srcDir); err == nil && len(remaining) == 0 {
		_ = os.Remove(srcDir)
	}

	return nil
}
