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

// SweepDisposition is what a completed run entitles a workitem to. It varies by
// type: the loop is the work for a bug or chore, but not for research or design.
type SweepDisposition string

const (
	// DispositionPromote: run completion retires the workitem.
	DispositionPromote SweepDisposition = "promote"
	// DispositionRoute: findings need a destination before the item is retired.
	DispositionRoute SweepDisposition = "route"
)

// Sweep skip reason codes: the stable machine-readable half of a reported skip.
const (
	SkipDesignAwaitsImplementation = "design_awaits_implementation"
	SkipRecentWrites               = "recent_writes"
	SkipLinkedScope                = "linked_scope"
	SkipNeedsRouting               = "needs_routing"
)

// WorkflowTypeResearch is a custom type, not a builtin, but carries explore's
// "findings need a home" semantics and must get the same disposition.
const WorkflowTypeResearch WorkflowType = "research"

// SweepCandidate is a workitem whose workflow run reached completed. Reason
// names the evidence kind so a future evidence tier can share this shape; today
// it is always EvidenceWorkflowRunCompleted.
type SweepCandidate struct {
	Item        WorkItem
	Reason      string
	Disposition SweepDisposition
	// RunID is the completed run whose terminal state made the item eligible.
	// Because fest clears active_run_id on completion, this is the workitem's
	// latest run (not an active run), carried through for the promote evidence.
	RunID string
}

// SweepSkip is a workitem the sweep deliberately left alone. Reason is one of
// the codes above; Detail is the sentence reported to the user.
type SweepSkip struct {
	Item   WorkItem
	Reason string
	Detail string
	RunID  string
}

// SweepPlan is one discovery pass classified: actionable, and deliberately not.
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
		if item.Completion != nil {
			if item.Completion.Policy == CompletionPolicyRecurring || item.Completion.ReviewedRunID == wf.LatestRunID {
				continue
			}
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

// DispositionOf maps a workflow type to its disposition. Design never reaches
// here; PlanSweep records it as a skip.
func DispositionOf(wt WorkflowType) SweepDisposition {
	switch wt {
	case WorkflowTypeExplore, WorkflowTypeResearch:
		return DispositionRoute
	default:
		return DispositionPromote
	}
}

// SweepBannerText returns the read-only banner reporting n workitems with
// completed runs awaiting a decision, or "" when n <= 0. Shared by camp wi and
// camp fresh (report mode) so the wording lives in exactly one place. It names
// --prompt: the bare command declines to touch explore and research items.
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

// InDungeonPath reports whether relPath contains a dungeon path segment. It
// compares whole segments (a workitem named "my-dungeon-notes" is not excluded)
// and is shared with triage so "outside dungeons" means one thing everywhere.
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
