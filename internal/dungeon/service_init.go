package dungeon

import (
	"context"

	dungeonscaffold "github.com/Obedience-Corp/camp/internal/dungeon/scaffold"
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
	result, err := dungeonscaffold.Init(ctx, s.dungeonPath, dungeonscaffold.InitOptions{
		Force: opts.Force,
	})
	if err != nil {
		return nil, err
	}

	return &InitResult{
		CreatedDirs:  result.CreatedDirs,
		CreatedFiles: result.CreatedFiles,
		Skipped:      result.Skipped,
	}, nil
}
