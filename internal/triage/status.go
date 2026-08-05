package triage

import (
	"context"
	"sort"
	"time"
)

// RowState is where one row stands in the run, derived from its manifest entry
// and its folded verdict.
type RowState string

const (
	// RowPendingEvidence means nothing has been submitted for the row yet.
	RowPendingEvidence RowState = "pending-evidence"
	// RowProposed means a proposal awaits approval.
	RowProposed RowState = "proposed"
	// RowApproved means the verdict is ready for apply.
	RowApproved RowState = "approved"
	// RowRejected means the proposal was refused and the row re-queues.
	RowRejected RowState = "rejected"
	// RowStale means refresh invalidated the verdict.
	RowStale RowState = "stale"
	// RowApplied means the verdict executed and a receipt exists.
	RowApplied RowState = "applied"
	// RowVerified means discovery confirmed the applied result.
	RowVerified RowState = "verified"
	// RowCarried means the verdict came forward from a base run.
	RowCarried RowState = "carried"
)

// RowStates returns the row-state vocabulary in lifecycle order.
func RowStates() []string {
	return []string{
		string(RowPendingEvidence),
		string(RowProposed),
		string(RowApproved),
		string(RowRejected),
		string(RowStale),
		string(RowApplied),
		string(RowVerified),
		string(RowCarried),
	}
}

// BatchProgress is one review batch's standing.
type BatchProgress struct {
	Batch    int `json:"batch"`
	Rows     int `json:"rows"`
	Decided  int `json:"decided"`
	Approved int `json:"approved"`
}

// Consolidation is one unfinished split from a `consolidate` verdict.
//
// The queue is always present in the payload and empty until the split verb
// exists, so a consumer written against this shape does not change when it
// starts filling.
type Consolidation struct {
	StableID   string   `json:"stable_id"`
	Successors []string `json:"successors"`
	Missing    []string `json:"missing"`
}

// Status is the whole answer `camp triage status` reports.
type Status struct {
	RunID   string  `json:"run_id"`
	Phase   Phase   `json:"phase"`
	Mode    RunMode `json:"mode"`
	Profile string  `json:"profile"`
	Active  bool    `json:"active"`
	// AbandonReason is set only on an abandoned run that recorded one.
	AbandonReason string `json:"abandon_reason,omitempty"`
	Rows          int    `json:"rows"`
	// Counts holds every state in RowStates(), including zeros, so a caller
	// can index it without checking for presence.
	Counts         map[string]int  `json:"counts"`
	Batches        []BatchProgress `json:"batches"`
	Consolidations []Consolidation `json:"consolidations"`
	IdentityIssues int             `json:"identity_exceptions"`
	CreatedAt      string          `json:"created_at"`
}

// BuildStatus derives a run's status from run data alone.
//
// No discovery walk happens here. Status answers "where is this session",
// which must be instant and must keep meaning even when the campaign has moved
// underneath the run. Comparing the session against the live campaign is what
// refresh is for, and conflating the two would make status quietly expensive
// and quietly wrong.
func BuildStatus(ctx context.Context, store *Store, runID string) (*Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	run, err := store.OpenRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	verdicts, err := store.Verdicts(ctx, runID)
	if err != nil {
		return nil, err
	}
	return StatusFrom(run, verdicts), nil
}

// StatusFrom folds a run and its verdicts into a status. Pure, so the shape
// can be tested without a filesystem.
func StatusFrom(run *Run, verdicts map[string]RowVerdict) *Status {
	status := &Status{
		RunID:          run.ID,
		Phase:          run.State.Phase,
		Mode:           run.Manifest.Mode,
		Profile:        run.Manifest.Profile.Name,
		Active:         run.Active(),
		Rows:           len(run.Manifest.Rows),
		Counts:         emptyCounts(),
		Batches:        []BatchProgress{},
		Consolidations: []Consolidation{},
		CreatedAt:      run.Manifest.CreatedAt.Format(time.RFC3339),
	}
	if run.State.AbandonReason != nil {
		status.AbandonReason = *run.State.AbandonReason
	}

	byBatch := map[int]*BatchProgress{}
	for _, row := range run.Manifest.Rows {
		state := rowStateFor(row, verdicts[row.StableID])
		status.Counts[string(state)]++
		if row.IdentityException != nil {
			status.IdentityIssues++
		}

		progress, ok := byBatch[row.Batch]
		if !ok {
			progress = &BatchProgress{Batch: row.Batch}
			byBatch[row.Batch] = progress
		}
		progress.Rows++
		switch state {
		case RowApproved, RowApplied, RowVerified, RowCarried:
			progress.Decided++
			progress.Approved++
		case RowRejected:
			progress.Decided++
		}
	}

	for _, batch := range sortedBatches(byBatch) {
		status.Batches = append(status.Batches, *batch)
	}
	return status
}

// rowStateFor decides a row's state from its manifest entry and verdict.
//
// A carried row is reported as carried even though it holds an approved
// verdict: the operator needs to know which decisions this run made and which
// it inherited, because those are the ones a changed anchor can invalidate.
func rowStateFor(row ManifestRow, verdict RowVerdict) RowState {
	if row.CarriedFrom != nil && verdict.State == VerdictNone {
		return RowCarried
	}
	switch verdict.State {
	case VerdictProposed:
		return RowProposed
	case VerdictApproved:
		return RowApproved
	case VerdictRejected:
		return RowRejected
	case VerdictStale:
		return RowStale
	case VerdictSuperseded:
		return RowPendingEvidence
	default:
		return RowPendingEvidence
	}
}

// emptyCounts seeds every state at zero so the JSON shape is fixed.
func emptyCounts() map[string]int {
	counts := make(map[string]int, len(RowStates()))
	for _, state := range RowStates() {
		counts[state] = 0
	}
	return counts
}

// sortedBatches returns batch progress in batch order.
func sortedBatches(byBatch map[int]*BatchProgress) []*BatchProgress {
	out := make([]*BatchProgress, 0, len(byBatch))
	for _, progress := range byBatch {
		out = append(out, progress)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Batch < out[b].Batch })
	return out
}
