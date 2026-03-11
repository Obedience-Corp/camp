package dungeon

import (
	"context"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Init creates the dungeon directory structure.
// This creates the flow-compatible dungeon structure:
// - dungeon/
// - dungeon/completed/
// - dungeon/archived/
// - dungeon/someday/
// - dungeon/OBEY.md
// This operation is idempotent unless Force is true.
func (s *Service) Init(ctx context.Context, opts InitOptions) (*InitResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	result := &InitResult{}

	// Create directories - flow-compatible structure
	dirs := []string{
		s.dungeonPath,
		filepath.Join(s.dungeonPath, "completed"),
		filepath.Join(s.dungeonPath, "archived"),
		filepath.Join(s.dungeonPath, "someday"),
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

	// Create OBEY.md template file only
	obeyPath := filepath.Join(s.dungeonPath, "OBEY.md")

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

	// Create .gitkeep in empty status directories
	statusDirs := []string{"completed", "archived", "someday"}
	for _, dir := range statusDirs {
		if err := ctx.Err(); err != nil {
			return nil, camperrors.Wrap(err, "context cancelled")
		}

		gitkeepPath := filepath.Join(s.dungeonPath, dir, ".gitkeep")
		if _, err := os.Stat(gitkeepPath); os.IsNotExist(err) {
			if err := os.WriteFile(gitkeepPath, []byte{}, 0644); err != nil {
				return nil, camperrors.Wrapf(err, "failed to create .gitkeep in %s", dir)
			}
			result.CreatedFiles = append(result.CreatedFiles, filepath.Join(dir, ".gitkeep"))
		}
	}

	return result, nil
}
