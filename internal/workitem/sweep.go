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

// SweepCandidate is a workitem eligible for tier-1 auto-promotion: its active
// workflow run reached completed. Reason names the evidence kind so a future
// evidence tier can share this shape; today it is always
// EvidenceWorkflowRunCompleted. ActiveRunID is the run whose completion made
// the item eligible, carried through for the promote event's evidence payload.
type SweepCandidate struct {
	Item        WorkItem
	Reason      string
	ActiveRunID string
}

// PlanSweep returns the subset of items eligible for tier-1 (loop-completion)
// auto-promotion. Pure function: no I/O, no mutation, no context to cancel.
// items is expected to come from Discover(), whose walk already excludes
// dungeon subtrees and never populates WorkflowMeta for intents or festivals;
// the checks below are still explicit so this rule survives a future change to
// how items are gathered.
func PlanSweep(items []WorkItem) []SweepCandidate {
	var out []SweepCandidate
	for _, item := range items {
		if !sweepEligibleType(item.WorkflowType) {
			continue
		}
		if item.WorkflowMeta == nil || item.WorkflowMeta.RunStatus != runStatusCompleted {
			continue
		}
		if item.WorkflowMeta.ActiveRunID == "" {
			continue
		}
		if inDungeonPath(item.RelativePath) {
			continue
		}
		out = append(out, SweepCandidate{
			Item:        item,
			Reason:      EvidenceWorkflowRunCompleted,
			ActiveRunID: item.WorkflowMeta.ActiveRunID,
		})
	}
	return out
}

// SweepBannerText returns the read-only banner reporting n workitems with
// completed runs awaiting sweep, or "" when n <= 0. Singular "workitem" for
// n == 1, matching spec doc 03's example wording. Shared by camp wi and camp
// fresh (report mode) so the wording lives in exactly one place.
func SweepBannerText(n int) string {
	if n <= 0 {
		return ""
	}
	noun, verb := "workitems", "have"
	if n == 1 {
		noun, verb = "workitem", "has"
	}
	return fmt.Sprintf("%d %s %s completed runs; run camp workitem sweep", n, noun, verb)
}

// sweepEligibleType excludes the workflow types that fest owns (festivals) or
// that have their own promote paths (intents, v1). This is an explicit
// scope-boundary rule per spec doc 03, not an accident of which discovery paths
// populate WorkflowMeta today.
func sweepEligibleType(wt WorkflowType) bool {
	return wt != WorkflowTypeFestival && wt != WorkflowTypeIntent
}

// inDungeonPath reports whether relPath contains a dungeon path segment.
// Discover() structurally cannot produce such an item, so this is a defensive
// guard for differently-sourced callers; it compares whole segments so a
// workitem literally named "my-dungeon-notes" is not falsely excluded.
func inDungeonPath(relPath string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(relPath), "/") {
		if seg == "dungeon" {
			return true
		}
	}
	return false
}
