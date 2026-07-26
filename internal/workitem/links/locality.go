package links

import (
	"os"
	"path/filepath"
	"strings"
)

// MachineLocal reports whether a missing scope target means "gone" or only
// "not here".
//
// links.yaml is tracked in git, so removing a row propagates to every machine
// the campaign syncs to. That makes the distinction load-bearing rather than
// cosmetic: deleting a row because its target is absent locally destroys a link
// that is correct elsewhere, and the deletion travels.
//
// Machine-local targets:
//
//   - worktrees. camp's own scaffold writes "# Git worktrees (machine-local)"
//     above the ignore rule it adds for them (internal/scaffold/repair.go), so
//     a worktree missing here says nothing about any other machine.
//   - submodules declared in .gitmodules but not checked out, which is the
//     normal state of a fresh clone before `camp sync`.
//
// Everything else -- workflow/, festivals/, campaign paths -- is tracked, so
// its absence is a fact everywhere and the row really is dead.
func MachineLocal(campaignRoot string, scope LinkScope) bool {
	if scope.Kind == ScopeWorktree {
		return true
	}
	if scope.Kind == ScopeProject {
		return declaredSubmodule(campaignRoot, scope.Path)
	}
	return false
}

// declaredSubmodule reports whether .gitmodules declares path, meaning an empty
// or absent directory is an uninitialized submodule rather than a deleted one.
func declaredSubmodule(campaignRoot, path string) bool {
	raw, err := os.ReadFile(filepath.Join(campaignRoot, ".gitmodules"))
	if err != nil {
		return false
	}
	want := "path = " + strings.TrimSuffix(path, "/")
	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// ScopeTargetExists reports whether a link's scope target is present on disk.
func ScopeTargetExists(campaignRoot string, scope LinkScope) bool {
	if scope.Path == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(campaignRoot, filepath.FromSlash(scope.Path)))
	return err == nil
}

// Dead reports whether a link is provably dead, and why.
//
// The bar is deliberately high, because links.yaml is tracked and an automatic
// removal propagates to every machine. Only one signal clears it: a scope
// target that is missing and not machine-local. That is a single stat of an
// explicit path -- no inference, no scan.
//
// A missing *workitem* is not used here even though it looks like the stronger
// signal. Answering it means a full tree walk (wkitem.Discover, with its own
// handling for stable ids, keys, and festival ids), and pruning on a scan means
// any gap in it, or a branch that happens not to carry a workitem, deletes live
// links. `camp workitem doctor` owns that case: it is an explicit user action,
// it reports before it acts, and it already auto-fixes.
func Dead(campaignRoot string, link Link) (reason string, dead bool) {
	if !MachineLocal(campaignRoot, link.Scope) && !ScopeTargetExists(campaignRoot, link.Scope) {
		return string(link.Scope.Kind) + " " + link.Scope.Path + " no longer exists", true
	}
	return "", false
}

// Pruned is a row PruneDead removed, with the reason it was safe to remove.
type Pruned struct {
	Link   Link
	Reason string
}

// PruneDead removes provably dead rows from the registry and returns them, so
// the caller can report what it dropped. Machine-local rows are never removed.
func PruneDead(campaignRoot string, l *Links) []Pruned {
	if l == nil {
		return nil
	}
	var removed []Pruned
	kept := l.Links[:0]
	for _, link := range l.Links {
		if reason, dead := Dead(campaignRoot, link); dead {
			removed = append(removed, Pruned{Link: link, Reason: reason})
			continue
		}
		kept = append(kept, link)
	}
	l.Links = kept
	return removed
}
