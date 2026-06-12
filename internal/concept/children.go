package concept

import (
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
)

// pickerHiddenChildren are workflow registry entries that should never appear in
// the concept picker submenu even though they live in the config registry. The
// intents workflow is config-managed but linking an intent to the intents
// directory is noise (design Q3).
var pickerHiddenChildren = map[string]bool{
	"intent":  true,
	"intents": true,
}

// isPickerHidden reports whether a child concept is filtered from the picker.
func isPickerHidden(name string) bool {
	return pickerHiddenChildren[name]
}

// childConcepts maps configured ConceptEntry children to Concepts.
func childConcepts(entries []config.ConceptEntry) []Concept {
	if len(entries) == 0 {
		return nil
	}
	children := make([]Concept, 0, len(entries))
	for _, e := range entries {
		children = append(children, Concept{
			Name:        e.Name,
			Path:        e.Path,
			Description: e.Description,
			MaxDepth:    e.Depth,
			Ignore:      e.Ignore,
			Children:    childConcepts(e.Children),
		})
	}
	return children
}

// childForSubpath matches the first segment of a drill subpath against the
// parent's configured children. A match means traversal continues under the
// child's own path and depth/ignore rules instead of the parent's path.
// Picker-hidden children never appear in the submenu, so they never remap.
func childForSubpath(parent *Concept, subpath string) (*Concept, string, bool) {
	first, rest, _ := strings.Cut(subpath, "/")
	for i := range parent.Children {
		ch := &parent.Children[i]
		if isPickerHidden(ch.Name) {
			continue
		}
		if ch.Name == first {
			return ch, rest, true
		}
	}
	return nil, "", false
}

// parentChildItems builds the picker items for a parent concept: configured
// children first (authoritative order/labels, each pointing at its own path),
// then any on-disk subdirectory under the parent path not already covered by a
// child name. The intents workflow is filtered out. countFn returns the
// child-count for a campaign-relative directory path.
func parentChildItems(parent *Concept, diskItems []Item, countFn func(relPath string) int) []Item {
	var items []Item
	covered := make(map[string]bool)

	for _, ch := range parent.Children {
		if isPickerHidden(ch.Name) {
			continue
		}
		items = append(items, Item{
			Name:          ch.Name,
			Path:          ch.Path,
			IsDir:         true,
			Children:      countFn(ch.Path),
			DrillDisabled: ch.MaxDepth != nil && *ch.MaxDepth == 0,
		})
		covered[ch.Name] = true
	}

	ignoreSet := makeIgnoreSet(parent.Ignore)
	for _, di := range diskItems {
		if !di.IsDir || covered[di.Name] || isPickerHidden(di.Name) {
			continue
		}
		if ignoreSet[di.Name] || ignoreSet[di.Name+"/"] {
			continue
		}
		items = append(items, di)
		covered[di.Name] = true
	}

	return items
}
