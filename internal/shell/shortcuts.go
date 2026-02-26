package shell

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
)

// completionEntry holds data for shell completion of a navigation shortcut.
type completionEntry struct {
	Key         string
	Description string
}

// navigationShortcuts returns sorted shortcut entries for shell completion.
// Only includes shortcuts with navigation paths (not command-only like "cfg").
func navigationShortcuts() []completionEntry {
	defaults := config.DefaultNavigationShortcuts()
	var entries []completionEntry
	for key, sc := range defaults {
		if !sc.IsNavigation() {
			continue
		}
		desc := sc.Path
		if sc.Description != "" {
			desc = sc.Description
		}
		entries = append(entries, completionEntry{Key: key, Description: desc})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})
	return entries
}

// zshShortcutTargets returns the zsh _describe targets array entries.
func zshShortcutTargets() string {
	entries := navigationShortcuts()
	var lines []string
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("      '%s:%s'", e.Key, e.Description))
	}
	return strings.Join(lines, "\n")
}

// bashShortcutWords returns space-separated shortcut keys for bash compgen -W.
func bashShortcutWords() string {
	entries := navigationShortcuts()
	var keys []string
	for _, e := range entries {
		keys = append(keys, e.Key)
	}
	return strings.Join(keys, " ")
}

// fishShortcutCompletions returns fish complete commands for cgo shortcuts.
func fishShortcutCompletions() string {
	entries := navigationShortcuts()
	var lines []string
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf(
			`complete -c cgo -n "__camp_is_first_arg" -a "%s" -d "%s"`,
			e.Key, e.Description,
		))
	}
	return strings.Join(lines, "\n")
}
