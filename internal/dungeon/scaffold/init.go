package scaffold

import (
	"context"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// StandardStatuses are the canonical status directories for a standard dungeon.
var StandardStatuses = []string{"completed", "archived", "someday"}

// InitOptions controls dungeon scaffolding behavior.
type InitOptions struct {
	Force bool
}

// InitResult captures what was created while scaffolding a dungeon.
type InitResult struct {
	CreatedDirs  []string
	CreatedFiles []string
	Skipped      []string
}

// Init ensures a standard dungeon has its canonical directories, OBEY.md, and gitkeeps.
func Init(ctx context.Context, dungeonPath string, opts InitOptions) (*InitResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	result := &InitResult{}

	dirs := []string{dungeonPath}
	for _, status := range StandardStatuses {
		dirs = append(dirs, filepath.Join(dungeonPath, status))
	}

	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, camperrors.Wrap(err, "context cancelled")
		}

		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, camperrors.Wrapf(err, "creating directory %s", dir)
			}
			result.CreatedDirs = append(result.CreatedDirs, dir)
		}
	}

	obeyPath := filepath.Join(dungeonPath, "OBEY.md")
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	exists := false
	if _, err := os.Stat(obeyPath); err == nil {
		exists = true
	}

	if exists && !opts.Force {
		result.Skipped = append(result.Skipped, obeyPath)
	} else {
		content, err := GetOBEYTemplate()
		if err != nil {
			return nil, camperrors.Wrapf(err, "reading template for %s", obeyPath)
		}
		if err := os.WriteFile(obeyPath, content, 0644); err != nil {
			return nil, camperrors.Wrapf(err, "writing %s", obeyPath)
		}
		result.CreatedFiles = append(result.CreatedFiles, obeyPath)
	}

	for _, status := range StandardStatuses {
		if err := ctx.Err(); err != nil {
			return nil, camperrors.Wrap(err, "context cancelled")
		}

		gitkeepPath := filepath.Join(dungeonPath, status, ".gitkeep")
		if _, err := os.Stat(gitkeepPath); os.IsNotExist(err) {
			if err := os.WriteFile(gitkeepPath, []byte{}, 0644); err != nil {
				return nil, camperrors.Wrapf(err, "failed to create .gitkeep in %s", status)
			}
			result.CreatedFiles = append(result.CreatedFiles, gitkeepPath)
		}
	}

	return result, nil
}
