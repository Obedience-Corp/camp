package workitem

import (
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// scopeLayout reads the campaign's configured projects and worktrees
// directories, which is what a worktree scope path is measured against when
// resolving the project that owns it. A nil config means "not loaded", which
// takes the camp defaults.
func scopeLayout(cfg *config.CampaignConfig) links.ScopeLayout {
	if cfg == nil {
		return links.ScopeLayout{}
	}
	paths := cfg.Paths()
	return links.LayoutFor(paths.Projects, paths.Worktrees)
}
