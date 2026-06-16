package shortcuts

import (
	"sort"

	"github.com/Obedience-Corp/camp/internal/config"
)

// ShortcutDiff holds the categorized differences between current and default shortcuts.
type ShortcutDiff struct {
	Missing  []string // default keys not in current config
	Stale    []string // auto keys in current config not in defaults
	Modified []string // same key, different path/concept (auto only)
	Custom   []string // user-defined shortcuts
	Matched  int      // count of shortcuts matching defaults
}

// NewUserShortcut builds a ShortcutConfig for a shortcut added via the CLI.
func NewUserShortcut(path, description, concept string) config.ShortcutConfig {
	return config.ShortcutConfig{
		Path:        path,
		Description: description,
		Concept:     concept,
		Source:      config.ShortcutSourceUser,
	}
}

// IsAutoShortcut returns true if the shortcut was auto-generated.
func IsAutoShortcut(sc config.ShortcutConfig, key string, defaults map[string]config.ShortcutConfig) bool {
	if sc.Source == config.ShortcutSourceUser {
		return false
	}
	if sc.Source == config.ShortcutSourceAuto {
		return true
	}
	if def, ok := defaults[key]; ok {
		return sc.Path == def.Path && sc.Concept == def.Concept
	}
	return false
}

// ComputeShortcutDiff compares current shortcuts against defaults.
func ComputeShortcutDiff(current, defaults map[string]config.ShortcutConfig) ShortcutDiff {
	var diff ShortcutDiff

	for key, def := range defaults {
		cur, exists := current[key]
		if !exists {
			diff.Missing = append(diff.Missing, key)
			continue
		}
		if cur.Path == def.Path && cur.Concept == def.Concept {
			diff.Matched++
		} else if IsAutoShortcut(cur, key, defaults) {
			diff.Modified = append(diff.Modified, key)
		} else {
			diff.Custom = append(diff.Custom, key)
		}
	}

	for key, sc := range current {
		if _, isDefault := defaults[key]; isDefault {
			continue
		}
		if IsAutoShortcut(sc, key, defaults) {
			diff.Stale = append(diff.Stale, key)
		} else {
			diff.Custom = append(diff.Custom, key)
		}
	}

	sort.Strings(diff.Missing)
	sort.Strings(diff.Stale)
	sort.Strings(diff.Modified)
	sort.Strings(diff.Custom)

	return diff
}
