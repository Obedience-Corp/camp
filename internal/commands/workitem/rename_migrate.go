package workitem

import (
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

// rekeyRenamePriorities moves the manual-priority and attention entries from
// oldKey to newKey, preserving each entry verbatim. Returns true when the store
// changed. Unlike gather's N:1 collapse this is a 1:1 remap, so the item keeps
// its stage and group across the rename.
func rekeyRenamePriorities(store *priority.Store, oldKey, newKey string) bool {
	if oldKey == newKey {
		return false
	}
	changed := false
	if entry, ok := store.ManualPriorities[oldKey]; ok {
		delete(store.ManualPriorities, oldKey)
		store.ManualPriorities[newKey] = entry
		changed = true
	}
	if entry, ok := store.Attention[oldKey]; ok {
		delete(store.Attention, oldKey)
		store.Attention[newKey] = entry
		changed = true
	}
	return changed
}

// rehomeRenameLinks repoints registry links from the renamed workitem's old
// path onto its new path. Two kinds of reference move:
//   - links whose subject is this workitem: WorkitemKey oldKey -> newKey. The
//     WorkitemID (stable id) is unchanged by a rename and is left untouched.
//   - scope paths that target the renamed directory, exactly or as a subtree:
//     rewritten from oldRel to newRel.
//
// Returns true when the registry changed.
func rehomeRenameLinks(reg *links.Links, oldKey, newKey, oldRel, newRel string) bool {
	oldRelSlash := filepath.ToSlash(oldRel)
	newRelSlash := filepath.ToSlash(newRel)
	changed := false
	for i := range reg.Links {
		l := &reg.Links[i]
		if oldKey != newKey && l.WorkitemKey == oldKey {
			l.WorkitemKey = newKey
			changed = true
		}
		if np, ok := rewriteScopePath(l.Scope.Path, oldRelSlash, newRelSlash); ok {
			l.Scope.Path = np
			changed = true
		}
	}
	return changed
}

// rewriteScopePath rewrites a link scope path that points at the renamed item.
// It matches the directory itself and any path beneath it, leaving unrelated
// scopes untouched.
func rewriteScopePath(p, oldRel, newRel string) (string, bool) {
	if oldRel == newRel {
		return p, false
	}
	if p == oldRel {
		return newRel, true
	}
	if strings.HasPrefix(p, oldRel+"/") {
		return newRel + strings.TrimPrefix(p, oldRel), true
	}
	return p, false
}
