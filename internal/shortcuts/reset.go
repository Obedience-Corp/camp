package shortcuts

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ResetPlan contains the loaded jumps config and computed reset changes.
type ResetPlan struct {
	Jumps    *config.JumpsConfig
	Defaults map[string]config.ShortcutConfig
	Diff     ShortcutDiff
}

// PrepareReset loads the jumps config and computes its diff from defaults.
func PrepareReset(ctx context.Context, root string) (*ResetPlan, error) {
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

	defaults := config.DefaultNavigationShortcuts()
	return &ResetPlan{
		Jumps:    jumps,
		Defaults: defaults,
		Diff:     ComputeShortcutDiff(jumps.Shortcuts, defaults),
	}, nil
}

// HasAutoDiff reports whether auto-generated shortcuts need reset work.
func (p *ResetPlan) HasAutoDiff() bool {
	return len(p.Diff.Missing) > 0 || len(p.Diff.Stale) > 0 || len(p.Diff.Modified) > 0
}

// ApplyAll replaces all shortcuts with defaults.
func (p *ResetPlan) ApplyAll() {
	p.Jumps.Shortcuts = p.Defaults
}

// ApplyIncremental applies missing, stale, and modified default shortcut changes.
func (p *ResetPlan) ApplyIncremental() {
	for _, key := range p.Diff.Missing {
		p.Jumps.Shortcuts[key] = p.Defaults[key]
	}
	for _, key := range p.Diff.Stale {
		delete(p.Jumps.Shortcuts, key)
	}
	for _, key := range p.Diff.Modified {
		p.Jumps.Shortcuts[key] = p.Defaults[key]
	}
}
