package shortcuts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// AddJumpInput describes a campaign-level shortcut to add or update.
type AddJumpInput struct {
	Name        string
	Path        string
	Description string
	Concept     string
}

// AddJumpPlan contains the validated update for a campaign-level shortcut.
type AddJumpPlan struct {
	Name     string
	Path     string
	Concept  string
	Shortcut config.ShortcutConfig
	Existing *config.ShortcutConfig
	Jumps    *config.JumpsConfig
}

// PrepareAddJump loads and validates the jumps update for a campaign-level shortcut.
func PrepareAddJump(ctx context.Context, root string, input AddJumpInput) (*AddJumpPlan, error) {
	if input.Path == "" && input.Concept == "" {
		return nil, fmt.Errorf("shortcut must have a path or concept (use -c to specify concept)")
	}

	jumps, err := config.LoadJumpsConfig(ctx, root)
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to load jumps config")
	}
	if jumps == nil {
		defaultJumps := config.DefaultJumpsConfig()
		jumps = &defaultJumps
	}
	if jumps.Shortcuts == nil {
		jumps.Shortcuts = make(map[string]config.ShortcutConfig)
	}

	if input.Path != "" {
		fullPath := filepath.Join(root, input.Path)
		if stat, err := os.Stat(fullPath); err != nil || !stat.IsDir() {
			return nil, fmt.Errorf("path does not exist or is not a directory: %s", fullPath)
		}
	}

	var existing *config.ShortcutConfig
	if sc, ok := jumps.Shortcuts[input.Name]; ok {
		existing = &sc
	}

	return &AddJumpPlan{
		Name:     input.Name,
		Path:     input.Path,
		Concept:  input.Concept,
		Shortcut: NewUserShortcut(input.Path, input.Description, input.Concept),
		Existing: existing,
		Jumps:    jumps,
	}, nil
}
