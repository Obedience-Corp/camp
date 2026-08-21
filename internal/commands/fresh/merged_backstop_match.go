package fresh

import (
	"context"
	"path/filepath"
	"strings"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

// listWorktreeBranchPaths records each live (non-detached) worktree's git
// branch → campaign-relative path. Capture this BEFORE prune: after prune the
// branch is gone and the worktree is often detached or removed, so git can no
// longer answer "which worktree held this branch."
func listWorktreeBranchPaths(ctx context.Context, root, projectPath string) map[string]string {
	if ctx.Err() != nil || projectPath == "" {
		return nil
	}
	entries, err := worktree.NewGitWorktree(projectPath).List(ctx)
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDetached || e.Branch == "" || e.Branch == "HEAD (detached)" {
			continue
		}
		out[e.Branch] = worktreeScopePath(root, e.Path)
	}
	return out
}

// worktreeScopePath converts a git worktree absolute path to the slash-form
// campaign-relative path stored on worktree-scope links.
func worktreeScopePath(root, abs string) string {
	if abs == "" {
		return ""
	}
	if root != "" {
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		if rel, err := filepath.Rel(root, abs); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filepath.Clean(abs))
}

func canonicalPrunedBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	branch = strings.TrimPrefix(branch, "refs/heads/")
	branch = strings.TrimPrefix(branch, "origin/")
	return branch
}

func lookupBranchPath(branchPaths map[string]string, branch string) (string, bool) {
	if len(branchPaths) == 0 || branch == "" {
		return "", false
	}
	if path, ok := branchPaths[branch]; ok && path != "" {
		return path, true
	}
	canon := canonicalPrunedBranch(branch)
	if canon != branch {
		if path, ok := branchPaths[canon]; ok && path != "" {
			return path, true
		}
	}
	return "", false
}

func sameScopePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.ToSlash(filepath.Clean(a)) == filepath.ToSlash(filepath.Clean(b))
}

// collectWorktreeLinkMatches maps pruned branches to active workitems via the
// pre-prune git worktree branch map (recorded fact), then directory basename
// only when git has no live worktree for that branch. At most one match per
// workitem key. Unmatched branches fall through to the commit-tag signal.
func collectWorktreeLinkMatches(all []links.Link, active []wkitem.WorkItem, prunedBranches []string, branchPaths map[string]string) (matches []MergedBackstopMatch, unmatched []string, matchedKeys map[string]bool) {
	matchedKeys = map[string]bool{}
	seenBranch := map[string]bool{}
	for _, raw := range prunedBranches {
		branch := canonicalPrunedBranch(raw)
		if branch == "" || seenBranch[branch] {
			continue
		}
		seenBranch[branch] = true
		wi, scopePath, ok := matchWorktreeLinkBranch(all, active, branch, branchPaths)
		if !ok {
			unmatched = append(unmatched, branch)
			continue
		}
		if matchedKeys[wi.Key] {
			// Another worktree of the same item merged. Not unmatched: that would
			// wrongly open the commit-tag path.
			continue
		}
		matches = append(matches, MergedBackstopMatch{
			Workitem:  wi,
			Branch:    branch,
			Signal:    SignalWorktreeLink,
			ScopePath: scopePath,
		})
		matchedKeys[wi.Key] = true
	}
	return matches, unmatched, matchedKeys
}

// matchWorktreeLinkBranch finds the active workitem for a pruned branch.
// Primary: the pre-prune git worktree list maps branch → path, then a
// worktree-scope link at that path. Fallback: directory basename equals the
// full branch name (covers leftover links after the worktree is already gone;
// does not strip feat/fix/chore prefixes).
func matchWorktreeLinkBranch(all []links.Link, active []wkitem.WorkItem, branch string, branchPaths map[string]string) (wkitem.WorkItem, string, bool) {
	if path, ok := lookupBranchPath(branchPaths, branch); ok {
		if wi, scope, found := matchWorktreeLinkPath(all, active, path); found {
			return wi, scope, true
		}
	}
	if _, mapped := lookupBranchPath(branchPaths, branch); mapped {
		// Git recorded a path for this branch that no workitem owns. Do not
		// guess from the directory name; that is the bug this matcher exists
		// to stop.
		return wkitem.WorkItem{}, "", false
	}
	for _, link := range all {
		if link.Scope.Kind != links.ScopeWorktree {
			continue
		}
		if filepath.Base(filepath.FromSlash(link.Scope.Path)) != branch {
			continue
		}
		if wi, ok := resolveLinkedActiveItem(link, active); ok {
			return wi, link.Scope.Path, true
		}
	}
	return wkitem.WorkItem{}, "", false
}

func matchWorktreeLinkPath(all []links.Link, active []wkitem.WorkItem, path string) (wkitem.WorkItem, string, bool) {
	for _, link := range all {
		if link.Scope.Kind != links.ScopeWorktree {
			continue
		}
		if !sameScopePath(link.Scope.Path, path) {
			continue
		}
		if wi, ok := resolveLinkedActiveItem(link, active); ok {
			return wi, link.Scope.Path, true
		}
	}
	return wkitem.WorkItem{}, "", false
}

// commitTagScopePath is the just-merged scope for a commit-tag match. If the
// workitem owns a worktree that git had on an unmatched pruned branch, that
// worktree is the merged scope (so HasOpenWork does not count it as other
// open work). Otherwise the project path: a true no-worktree quick fix.
func commitTagScopePath(wi wkitem.WorkItem, all []links.Link, projectScope string, unmatched []string, branchPaths map[string]string) string {
	for _, branch := range unmatched {
		path, ok := lookupBranchPath(branchPaths, branch)
		if !ok {
			continue
		}
		if workitemOwnsWorktreePath(wi, all, path) {
			return path
		}
	}
	return projectScope
}

func workitemOwnsWorktreePath(wi wkitem.WorkItem, all []links.Link, path string) bool {
	for _, link := range all {
		if link.Scope.Kind != links.ScopeWorktree {
			continue
		}
		if !linkMatchesBackstopItem(link, wi) {
			continue
		}
		if sameScopePath(link.Scope.Path, path) {
			return true
		}
	}
	return false
}
