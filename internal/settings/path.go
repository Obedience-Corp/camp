package settings

import (
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// CatalogPath renders the display path for a settings entry.
//
// Local entries render campaign-root-relative in .campaign/... form (never
// absolute, never CWD-relative). Global entries already carry their resolved
// real path (honoring $CAMP_REGISTRY_PATH / $XDG_CONFIG_HOME from the builder),
// so this only presents it: a leading $HOME collapses to ~, and paths outside
// $HOME are shown absolute with no false ~.
func CatalogPath(e SettingEntry, campaignRoot string) string {
	switch e.Scope {
	case ScopeLocal:
		p := e.Path
		if filepath.IsAbs(p) {
			if rel, err := filepath.Rel(campaignRoot, p); err == nil {
				p = rel
			}
		}
		return filepath.ToSlash(p)
	case ScopeGlobal:
		return pathutil.AbbreviateHome(e.Path)
	default:
		return e.Path
	}
}
