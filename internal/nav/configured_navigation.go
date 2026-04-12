package nav

import (
	"path"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
)

// ConfiguredTarget represents a first-argument navigation target resolved
// from campaign configuration.
type ConfiguredTarget struct {
	Category     Category
	RelativePath string
	Query        string
	Drill        bool
	Matched      bool
}

// PathAliasTarget represents a long-form directory alias derived from a
// configured navigation shortcut target path.
type PathAliasTarget struct {
	Category     Category
	RelativePath string
}

// NormalizeNavigationName normalizes a navigation token for matching.
func NormalizeNavigationName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.TrimRight(name, "/")
	return name
}

// FindConceptEntry resolves a configured concept by name using normalized matching.
func FindConceptEntry(concepts []config.ConceptEntry, name string) (config.ConceptEntry, bool) {
	normalized := NormalizeNavigationName(name)
	for _, concept := range concepts {
		if NormalizeNavigationName(concept.Name) == normalized {
			return concept, true
		}
	}
	return config.ConceptEntry{}, false
}

// TopLevelNavigationNames returns the union of configured shortcut keys,
// long-form directory aliases derived from navigation shortcuts, and configured
// concept names.
func TopLevelNavigationNames(cfg *config.CampaignConfig) []string {
	if cfg == nil {
		return nil
	}

	seen := make(map[string]string)
	add := func(name string) {
		normalized := NormalizeNavigationName(name)
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = name
	}

	for key, shortcut := range cfg.Shortcuts() {
		if shortcut.IsNavigation() {
			add(key)
		}
	}

	for alias := range BuildPathAliasMappings(cfg.Shortcuts()) {
		add(alias)
	}

	for _, concept := range cfg.Concepts() {
		add(concept.Name)
	}

	names := make([]string, 0, len(seen))
	for _, name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolveConfiguredTarget resolves the first navigation argument against the
// current campaign configuration.
//
// Resolution order:
//  1. Configured shortcut keys from jumps.yaml
//  2. Configured concept names from campaign.yaml
//  3. Long-form directory aliases derived from configured navigation shortcuts
func ResolveConfiguredTarget(cfg *config.CampaignConfig, args []string) ConfiguredTarget {
	if cfg == nil {
		return ConfiguredTarget{}
	}

	parsedArgs, shortcutDrill := splitShortcutDrillArgs(args)
	if shortcutDrill {
		shortcutMappings := BuildCategoryMappings(cfg.Shortcuts())
		parsed := ParseShortcut(parsedArgs, shortcutMappings)
		if parsed.IsShortcut {
			return ConfiguredTarget{
				Category: parsed.Category,
				Query:    parsed.Query,
				Drill:    true,
				Matched:  true,
			}
		}
	}

	shortcutMappings := BuildCategoryMappings(cfg.Shortcuts())
	parsed := ParseShortcut(args, shortcutMappings)
	if parsed.IsShortcut {
		return ConfiguredTarget{
			Category: parsed.Category,
			Query:    parsed.Query,
			Matched:  true,
		}
	}

	if len(args) == 0 {
		return ConfiguredTarget{}
	}

	if drillArgs, ok := splitSlashDrillArgs(args); ok {
		target := resolveDrillTarget(cfg, drillArgs)
		target.Drill = target.Matched
		return target
	}

	return resolveDrillTarget(cfg, args)
}

func resolveDrillTarget(cfg *config.CampaignConfig, args []string) ConfiguredTarget {
	token := args[0]
	query := ""
	if len(args) > 1 {
		query = strings.Join(args[1:], " ")
	}

	if concept, ok := FindConceptEntry(cfg.Concepts(), token); ok {
		if cat, ok := CategoryForStandardPath(concept.Path); ok {
			return ConfiguredTarget{
				Category: cat,
				Query:    query,
				Matched:  true,
			}
		}
		return ConfiguredTarget{
			RelativePath: concept.Path,
			Query:        query,
			Matched:      true,
		}
	}

	if alias, ok := BuildPathAliasMappings(cfg.Shortcuts())[NormalizeNavigationName(token)]; ok {
		return ConfiguredTarget{
			Category:     alias.Category,
			RelativePath: alias.RelativePath,
			Query:        query,
			Matched:      true,
		}
	}

	return ConfiguredTarget{}
}

func splitSlashDrillArgs(args []string) ([]string, bool) {
	if len(args) == 0 {
		return nil, false
	}

	first := args[0]
	if !strings.Contains(first, "/") {
		return nil, false
	}

	parts := strings.SplitN(first, "/", 2)
	prefix := parts[0]
	remainder := parts[1]
	if prefix == "" {
		return nil, false
	}

	drillArgs := []string{prefix}
	if remainder != "" {
		drillArgs = append(drillArgs, remainder)
	}
	if len(args) > 1 {
		drillArgs = append(drillArgs, args[1:]...)
	}
	return drillArgs, true
}

func splitShortcutDrillArgs(args []string) ([]string, bool) {
	if len(args) == 0 {
		return nil, false
	}

	first := args[0]
	if !strings.Contains(first, "@") {
		return nil, false
	}

	parts := strings.SplitN(first, "@", 2)
	shortcut := parts[0]
	remainder := parts[1]
	if shortcut == "" {
		return nil, false
	}

	drillArgs := []string{shortcut}
	if remainder != "" {
		drillArgs = append(drillArgs, remainder)
	}
	if len(args) > 1 {
		drillArgs = append(drillArgs, args[1:]...)
	}
	return drillArgs, true
}

// BuildPathAliasMappings derives long-form directory aliases from configured
// navigation shortcut target paths. For example:
//   - "workflow/design/" -> "design"
//   - ".campaign/intents/" -> "intents"
//   - "ai_docs/" -> "ai_docs"
//
// Ambiguous aliases are dropped rather than silently picking one target.
func BuildPathAliasMappings(shortcuts map[string]config.ShortcutConfig) map[string]PathAliasTarget {
	aliases := make(map[string]PathAliasTarget)
	ambiguous := make(map[string]struct{})

	keys := make([]string, 0, len(shortcuts))
	for key := range shortcuts {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		shortcut := shortcuts[key]
		if !shortcut.IsNavigation() {
			continue
		}

		alias := PathAliasForPath(shortcut.Path)
		if alias == "" {
			continue
		}
		if _, skip := ambiguous[alias]; skip {
			continue
		}

		target := PathAliasTarget{RelativePath: shortcut.Path}
		if category, ok := CategoryForStandardPath(shortcut.Path); ok {
			target.Category = category
		}

		if existing, ok := aliases[alias]; ok {
			if existing.RelativePath != target.RelativePath || existing.Category != target.Category {
				delete(aliases, alias)
				ambiguous[alias] = struct{}{}
			}
			continue
		}

		aliases[alias] = target
	}

	return aliases
}

// PathAliasForPath returns the trailing directory name for a configured path.
func PathAliasForPath(relativePath string) string {
	cleaned := strings.TrimSpace(relativePath)
	cleaned = strings.TrimRight(cleaned, "/")
	if cleaned == "" {
		return ""
	}

	alias := path.Base(cleaned)
	if alias == "." || alias == "/" {
		return ""
	}
	return NormalizeNavigationName(alias)
}
