package workitem

import (
	"fmt"
	"path/filepath"
	"strings"
)

// EvidenceWorkflowRunCompleted names the tier-1 (loop-completion) evidence
// kind. It is the only auto-promotion evidence tier; merged-branch evidence
// (tier 2) prompts or reports and never reuses this constant.
const EvidenceWorkflowRunCompleted = "workflow_run_completed"

// EvidenceMergedBranch names the tier-2 (inference) evidence kind: a branch or
// worktree linked to the workitem merged. It never auto-promotes; a human
// accepting a camp fresh prompt is the only path that records it.
const EvidenceMergedBranch = "merged_branch"

// runStatusCompleted is the RunStatus value the localrun replay assigns after a
// workflow_run_completed event (internal/workitem/localrun.go).
const runStatusCompleted = "completed"

// SweepDisposition says what a completed run entitles a workitem to, which
// depends on what kind of work the item tracks.
//
// A finished authoring loop is not the same claim for every type. For a bug or
// a chore, the loop was the work, so finishing it finishes the item. For
// explore or research, finishing the loop means findings now exist and need a
// home; moving the directory to a dungeon at that moment buries them. For a
// design, the loop produced a specification that nothing has implemented yet,
// so run completion is not evidence of completion at all.
type SweepDisposition string

const (
	// DispositionPromote: the completed run is sufficient evidence to retire the
	// workitem. Bug, chore, feature, and custom types.
	DispositionPromote SweepDisposition = "promote"
	// DispositionRoute: findings exist and need a destination before the item is
	// retired, so this asks where they go instead of moving anything. Explore
	// and research.
	DispositionRoute SweepDisposition = "route"
)

// Sweep skip reason codes. These are the stable machine-readable half of a
// reported skip; the accompanying detail is the sentence a person reads.
const (
	// SkipDesignAwaitsImplementation: a design is done when it is built, so a
	// completed authoring run never promotes one.
	SkipDesignAwaitsImplementation = "design_awaits_implementation"
	// SkipRecentWrites: something wrote inside the directory within
	// FreshWriteWindow, so a session is probably still working there.
	SkipRecentWrites = "recent_writes"
	// SkipLinkedScope: a link is scoped inside the directory, so automatic mode
	// refuses to move it out from under whoever holds that link.
	SkipLinkedScope = "linked_scope"
	// SkipNeedsRouting: an explore or research item reached automatic mode,
	// which has no answer for where the findings should go.
	SkipNeedsRouting = "needs_routing"
)

// WorkflowTypeResearch is the conventional custom type for research items. It
// is not a builtin (no dedicated discovery path), but it carries the same
// "findings need a home" semantics as explore, so the planner treats the two
// alike rather than letting a naming choice decide whether notes get buried.
const WorkflowTypeResearch WorkflowType = "research"

// SweepCandidate is a workitem whose workflow run reached completed. Reason
// names the evidence kind so a future evidence tier can share this shape; today
// it is always EvidenceWorkflowRunCompleted. Disposition says what that evidence
// entitles the item to, which the caller honors according to its mode.
type SweepCandidate struct {
	Item        WorkItem
	Reason      string
	Disposition SweepDisposition
	// RunID is the completed run whose terminal state made the item eligible.
	// Because fest clears active_run_id on completion, this is the workitem's
	// latest run (not an active run), carried through for the promote evidence.
	RunID string
}

// SweepSkip is a workitem the sweep looked at and deliberately left alone.
// Reason is one of the stable codes above and Detail is the sentence explaining
// it. Skips are reported, never silent: camp acting automatically is only
// acceptable while it says what it did and did not do.
type SweepSkip struct {
	Item   WorkItem
	Reason string
	Detail string
	RunID  string
}

// SweepPlan is the result of reading one discovery pass: what a completed run
// makes actionable, and what it deliberately does not.
type SweepPlan struct {
	Candidates []SweepCandidate
	Skipped    []SweepSkip
}

// PlanSweep classifies items whose workflow run completed. Pure function: no
// I/O, no mutation, no context to cancel. items is expected to come from
// Discover(), whose walk already excludes dungeon subtrees and never populates
// WorkflowMeta for intents or festivals; the checks below are still explicit so
// this rule survives a future change to how items are gathered.
func PlanSweep(items []WorkItem) SweepPlan {
	var plan SweepPlan
	for _, item := range items {
		if !sweepEligibleType(item.WorkflowType) {
			continue
		}
		wf := item.WorkflowMeta
		if wf == nil {
			continue
		}
		// Eligible iff the LATEST run reached completed AND no newer run is
		// active. fest clears active_run_id on completion, so a genuinely
		// completed workitem has ActiveRunID == "" and LatestRunStatus ==
		// "completed"; a run started afterward repoints ActiveRunID and makes it
		// ineligible again (the multi-run caveat).
		if wf.ActiveRunID != "" {
			continue
		}
		if wf.LatestRunStatus != runStatusCompleted || wf.LatestRunID == "" {
			continue
		}
		if InDungeonPath(item.RelativePath) {
			continue
		}
		if item.WorkflowType == WorkflowTypeDesign {
			plan.Skipped = append(plan.Skipped, SweepSkip{
				Item:   item,
				Reason: SkipDesignAwaitsImplementation,
				Detail: "design awaits implementation evidence (a merged branch or a completed festival)",
				RunID:  wf.LatestRunID,
			})
			continue
		}
		plan.Candidates = append(plan.Candidates, SweepCandidate{
			Item:        item,
			Reason:      EvidenceWorkflowRunCompleted,
			Disposition: DispositionOf(item.WorkflowType),
			RunID:       wf.LatestRunID,
		})
	}
	return plan
}

// DispositionOf maps a workflow type to what a completed run entitles it to.
// Design never reaches here: PlanSweep records it as a skip instead, because
// there is no disposition that a completed authoring run justifies for a spec
// nobody has built yet.
func DispositionOf(wt WorkflowType) SweepDisposition {
	switch wt {
	case WorkflowTypeExplore, WorkflowTypeResearch:
		return DispositionRoute
	default:
		return DispositionPromote
	}
}

// SweepBannerText returns the read-only banner reporting n workitems with
// completed runs awaiting a decision, or "" when n <= 0. Singular "workitem" for
// n == 1, matching spec doc 03's example wording. Shared by camp wi and camp
// fresh (report mode) so the wording lives in exactly one place.
//
// It names the prompting form of the command, because that is the one that
// handles every type correctly: the bare command auto-promotes and therefore
// declines to touch explore and research items at all.
func SweepBannerText(n int) string {
	if n <= 0 {
		return ""
	}
	noun, verb := "workitems", "have"
	if n == 1 {
		noun, verb = "workitem", "has"
	}
	return fmt.Sprintf("%d %s %s completed runs; run camp workitem sweep --prompt", n, noun, verb)
}

// sweepEligibleType excludes the workflow types that fest owns (festivals) or
// that have their own promote paths (intents, v1). This is an explicit
// scope-boundary rule per spec doc 03, not an accident of which discovery paths
// populate WorkflowMeta today.
func sweepEligibleType(wt WorkflowType) bool {
	return wt != WorkflowTypeFestival && wt != WorkflowTypeIntent
}

// InDungeonPath reports whether relPath contains a dungeon path segment.
// Discover() structurally cannot produce such an item, so this is a defensive
// guard for differently-sourced callers; it compares whole segments so a
// workitem literally named "my-dungeon-notes" is not falsely excluded.
//
// Exported because triage's staleness diff asks the same question when it
// decides a row is gone, and "outside dungeons" has to mean the same thing in
// both places or a row could be swept here and reported gone there.
func InDungeonPath(relPath string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(relPath), "/") {
		// Both spellings, because both exist in the wild: campaigns created
		// before `camp dungeon migrate` use "dungeon", and migrated ones use
		// the hidden ".dungeon". Matching only one made this silently answer
		// "not dungeoned" for half of all campaigns.
		if seg == "dungeon" || seg == ".dungeon" {
			return true
		}
	}
	return false
}
