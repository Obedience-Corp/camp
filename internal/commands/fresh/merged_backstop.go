package fresh

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	wkcmd "github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

// backstopRoot resolves the campaign root for the backstop: the threaded
// campRoot when set, else a cwd detect (fresh runs at or under the campaign
// root). Empty on failure disables the backstop for this project.
func backstopRoot(ctx context.Context, opts freshOptions) string {
	if opts.campRoot != "" {
		return opts.campRoot
	}
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return ""
	}
	return root
}

// reportMergedBackstop runs the tier-2 merged-branch backstop for one project
// after prune: map the just-deleted branches to still-active workitems, drop
// matches with open work elsewhere, and (mode "report", and "prompt" until the
// interactive prompt lands in 02_fresh_prompt_flow) print the exact promote
// command for each surviving match. Mode "off" or an empty root/branch list is
// a no-op. Inference evidence never auto-promotes; a failure is reported to out
// and never fails the fresh cycle.
func reportMergedBackstop(ctx context.Context, out io.Writer, root, projectPath string, deletedBranches []string, beforeSHA, mode string) {
	if mode == "off" || root == "" || len(deletedBranches) == 0 {
		return
	}
	matches, err := MapMergedBranchesToWorkitems(ctx, root, projectPath, deletedBranches, beforeSHA)
	if err != nil {
		_, _ = fmt.Fprintf(out, "%s merged-branch backstop skipped: %v\n", ui.WarningIcon(), err)
		return
	}
	if len(matches) == 0 {
		return
	}
	registry, err := links.Load(ctx, root)
	if err != nil {
		_, _ = fmt.Fprintf(out, "%s merged-branch backstop skipped: %v\n", ui.WarningIcon(), err)
		return
	}
	for _, m := range matches {
		if HasOpenWork(root, registry, m.Workitem, m.ScopePath, projectRelPath(root, projectPath)) {
			continue
		}
		_, _ = fmt.Fprintf(out, "%s workitem %s had a merged branch and is still active; promote when done:\n    %s\n",
			ui.InfoIcon(), backstopWorkitemLabel(m.Workitem), backstopPromoteCommand(m.Workitem))
	}
}

// backstopPromoteCommand renders the exact, copy-pasteable promote command using
// the workitem's resolvable id (StableID), not its internal Key.
func backstopPromoteCommand(wi wkitem.WorkItem) string {
	return "camp workitem promote " + backstopWorkitemID(wi) + " --target completed"
}

func backstopWorkitemID(wi wkitem.WorkItem) string {
	if wi.StableID != "" {
		return wi.StableID
	}
	return wi.Key
}

func backstopWorkitemLabel(wi wkitem.WorkItem) string {
	if wi.Title != "" {
		return wi.Title
	}
	return backstopWorkitemID(wi)
}

// Tier-2 evidence signals. A merged branch is inference-tier evidence: it
// prompts or reports, never auto-promotes (FESTIVAL_RULES rule 2).
const (
	// SignalWorktreeLink means a pruned branch's name matched a worktree-scope
	// link's directory basename (high confidence: the branch and its worktree
	// dir share a name by construction at `camp workitem worktree` time).
	SignalWorktreeLink = "worktree_link"
	// SignalCommitTag means a WI- tag was found on a commit newly reachable
	// from the default branch since this fresh cycle's pull (the fallback for
	// quick fixes committed without a worktree).
	SignalCommitTag = "commit_tag"
)

// MergedBackstopMatch is a still-active workitem whose merged branch (or merged
// WI-tagged commit) was observed by camp fresh's prune step. It is inference
// evidence: sequence 02_fresh_prompt_flow prompts on it, never auto-promotes.
type MergedBackstopMatch struct {
	Workitem wkitem.WorkItem
	// Branch is the pruned branch that matched, for worktree-link matches.
	// Empty for commit-tag matches, where the branch is already deleted and the
	// evidence is the merged commit reachable from the default branch.
	Branch string
	Signal string
	// ScopePath is the campaign-relative scope whose work just merged: the
	// worktree link's path for a worktree match, or the project path for a
	// commit-tag match. The open-work guard uses it to avoid counting the
	// just-merged scope itself as "other open work."
	ScopePath string
}

// MapMergedBranchesToWorkitems maps the branches camp fresh's prune step just
// deleted for one project back to still-active workitems. Two signals, in
// order of confidence: worktree-scope links (branch name == worktree dir
// basename), then WI- commit tags on commits newly reachable from the default
// branch since beforeSHA (captured before the pull, since the pruned branch ref
// is gone by the time prune returns). Festivals and intents are excluded per
// doc 03's scope boundary. Pure of prompt/UI concerns; git calls are I/O so it
// takes ctx. Returns no error on "no matches": absence of evidence is not an
// error.
func MapMergedBranchesToWorkitems(ctx context.Context, root, projectPath string, prunedBranches []string, beforeSHA string) ([]MergedBackstopMatch, error) {
	if len(prunedBranches) == 0 {
		return nil, nil
	}

	registry, err := links.Load(ctx, root)
	if err != nil {
		return nil, camperrors.Wrap(err, "load link registry")
	}
	cfg, _, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "load campaign config")
	}
	items, err := wkitem.Discover(ctx, root, paths.NewResolverFromConfig(root, cfg))
	if err != nil {
		return nil, camperrors.Wrap(err, "discover workitems")
	}
	active := activeBackstopItems(items)

	var matches []MergedBackstopMatch
	matchedKeys := map[string]bool{}
	var unmatchedBranches []string

	projectScope := projectRelPath(root, projectPath)

	// Signal 1: worktree-scope links, per pruned branch.
	for _, branch := range prunedBranches {
		wi, scopePath, ok := matchWorktreeLinkBranch(registry.Links, active, branch)
		if !ok {
			unmatchedBranches = append(unmatchedBranches, branch)
			continue
		}
		matches = append(matches, MergedBackstopMatch{Workitem: wi, Branch: branch, Signal: SignalWorktreeLink, ScopePath: scopePath})
		matchedKeys[wi.Key] = true
	}

	// Signal 2: WI- commit tags on commits newly reachable from the default
	// branch, only when some pruned branch had no worktree link. beforeSHA is
	// required; without it the newly-reachable set is unknown, so skip.
	if len(unmatchedBranches) > 0 && beforeSHA != "" {
		refs, scanErr := newlyMergedWorkitemRefs(ctx, projectPath, beforeSHA)
		if scanErr != nil {
			return matches, scanErr
		}
		for _, wi := range active {
			if matchedKeys[wi.Key] {
				continue
			}
			if refMatchesActiveItem(refs, wi) {
				matches = append(matches, MergedBackstopMatch{Workitem: wi, Signal: SignalCommitTag, ScopePath: projectScope})
				matchedKeys[wi.Key] = true
			}
		}
	}

	return matches, nil
}

// activeBackstopItems returns the discovered items eligible for tier-2 matching:
// everything Discover produced except festivals and intents (doc 03 scope
// boundary). Discover already excludes dungeon subtrees, so these are active.
func activeBackstopItems(items []wkitem.WorkItem) []wkitem.WorkItem {
	out := make([]wkitem.WorkItem, 0, len(items))
	for _, item := range items {
		if item.WorkflowType == wkitem.WorkflowTypeFestival || item.WorkflowType == wkitem.WorkflowTypeIntent {
			continue
		}
		out = append(out, item)
	}
	return out
}

// matchWorktreeLinkBranch finds the active workitem whose worktree-scope link's
// directory basename equals branch. Only ScopeWorktree links carry a
// branch<->workitem correlation (via the shared name at creation); repo/project
// scope links carry none, so they are ignored here.
func matchWorktreeLinkBranch(all []links.Link, active []wkitem.WorkItem, branch string) (wkitem.WorkItem, string, bool) {
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

// projectRelPath returns projectPath relative to the campaign root, slash-form.
func projectRelPath(root, projectPath string) string {
	rel, err := filepath.Rel(root, projectPath)
	if err != nil {
		return projectPath
	}
	return filepath.ToSlash(rel)
}

// resolveLinkedActiveItem resolves a link to a still-active workitem by stable
// id or key, matching worktree.go's linkMatchesWorkitem.
func resolveLinkedActiveItem(link links.Link, active []wkitem.WorkItem) (wkitem.WorkItem, bool) {
	for _, wi := range active {
		if wi.StableID != "" && link.WorkitemID == wi.StableID {
			return wi, true
		}
		if wi.Key != "" && link.WorkitemKey == wi.Key {
			return wi, true
		}
	}
	return wkitem.WorkItem{}, false
}

// newlyMergedWorkitemRefs scans commit subjects reachable from HEAD but not from
// beforeSHA in projectPath and returns the set of WI- refs their campaign tags
// carry. Scoped to one repo: fresh already operates one project at a time.
func newlyMergedWorkitemRefs(ctx context.Context, projectPath, beforeSHA string) (map[string]bool, error) {
	out, err := git.Output(ctx, projectPath, "log", beforeSHA+"..HEAD", "--pretty=format:%s")
	if err != nil {
		return nil, camperrors.Wrapf(err, "scan newly merged commits in %s", projectPath)
	}
	return workitemRefsFromSubjects(strings.Split(out, "\n")), nil
}

// workitemRefsFromSubjects extracts the set of WI- refs carried by the campaign
// tags on the given commit subjects. Pure: no I/O.
func workitemRefsFromSubjects(subjects []string) map[string]bool {
	refs := map[string]bool{}
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			continue
		}
		if ref := commitkit.ParseTag(subject).WorkitemRef; ref != "" {
			refs[ref] = true
		}
	}
	return refs
}

// refMatchesActiveItem reports whether any newly-merged WI- ref names wi, using
// the same alias resolution camp workitem commits uses (ref, stable id, key,
// slug). wi's own ref lives in SourceMetadata["ref"].
func refMatchesActiveItem(refs map[string]bool, wi wkitem.WorkItem) bool {
	aliases := wkcmd.WorkitemAliases(refFromItem(wi), &wi)
	for ref := range refs {
		if aliases[ref] {
			return true
		}
	}
	return false
}

func refFromItem(wi wkitem.WorkItem) string {
	if wi.SourceMetadata == nil {
		return ""
	}
	if ref, ok := wi.SourceMetadata["ref"].(string); ok {
		return ref
	}
	return ""
}

// HasOpenWork reports whether wi still has open work beyond the branch that just
// merged, in which case a tier-2 match must be suppressed (doc 03 conservatism
// guard: one merged PR out of several open ones is progress, not completion). A
// workitem linked to a DIFFERENT project/repo, or with a still-existing worktree
// other than the just-merged one, is "open." A stale worktree link whose
// directory is already gone does not count as open. mergedScopePath is the
// scope that just merged (the matched worktree link's path, or the project path
// for a commit-tag match); projectPath is the just-pruned project's
// campaign-relative path, so the just-merged project's own project/repo link is
// not mistaken for other open work.
func HasOpenWork(root string, registry *links.Links, wi wkitem.WorkItem, mergedScopePath, projectPath string) bool {
	for _, link := range registry.Links {
		if !linkMatchesBackstopItem(link, wi) {
			continue
		}
		switch link.Scope.Kind {
		case links.ScopeProject, links.ScopeRepo:
			// A project/repo link is open work only when it points at a
			// DIFFERENT project than the one whose branch just merged. The
			// just-merged project's own link is not "other" work (compare
			// against projectPath, not the matched worktree's scope path).
			if link.Scope.Path != projectPath {
				return true
			}
		case links.ScopeWorktree:
			// Skip the worktree that just merged; any OTHER worktree that still
			// exists on disk is open work.
			if link.Scope.Path == mergedScopePath {
				continue
			}
			if worktreeDirExists(root, link.Scope.Path) {
				return true
			}
		}
	}
	return false
}

func linkMatchesBackstopItem(link links.Link, wi wkitem.WorkItem) bool {
	if wi.StableID != "" && link.WorkitemID == wi.StableID {
		return true
	}
	return wi.Key != "" && link.WorkitemKey == wi.Key
}

func worktreeDirExists(root, scopePath string) bool {
	abs := scopePath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(scopePath))
	}
	info, err := os.Stat(abs)
	return err == nil && info.IsDir()
}
