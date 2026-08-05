package triage

import (
	"context"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// RefreshInput is one refresh pass over a run.
type RefreshInput struct {
	RunID string
	// Items is a fresh, UNSCOPED discovery walk over the whole campaign.
	// Refresh applies the run's own frozen scope itself, so no caller can
	// narrow the walk differently than the run was started with. Passing the
	// walk in rather than doing it here keeps the function testable without a
	// filesystem.
	Items []workitem.WorkItem
	// Actor records who ran the refresh on any stale event it appends.
	Actor string
	Now   time.Time
}

// RefreshResult is what a refresh classified and what it did about it.
type RefreshResult struct {
	RunID string
	Diff  Diff
	// StaleRecorded lists rows whose live verdict this refresh invalidated.
	// It is a subset of the changed and gone rows: a row with no live verdict
	// has nothing to retire, so it is classified without an event.
	StaleRecorded []string
	// Rekeyed lists moved rows whose manifest entry was updated in place.
	Rekeyed []string
	// Appended lists new rows added to the manifest.
	Appended []string
}

// RowsWithUncheckedAnchors reports how many rows carry an anchor refresh could
// not verify, so the caller can say "nothing changed" and "we could not look"
// as different things.
func (r *RefreshResult) RowsWithUncheckedAnchors() int {
	return r.Diff.RowsWithUncheckedAnchors()
}

// Refresh re-checks a run against the world and records what moved.
//
// The sequence is: index the discovery walk, collect each row's anchors from
// its evidence record, re-check the local ones, classify (purely), then apply
// the effects spec doc 04 assigns to each class — stale events for changed and
// gone rows, re-keys for moved rows, appends for new ones.
//
// The whole pass holds the manifest lock. Refresh reads the manifest, decides
// from it, and writes it back, so releasing the lock in between would let a
// concurrent refresh classify against a manifest this one is about to replace.
// The window is one local walk plus one hash pass, which is the cost bound the
// design sets anyway.
func (s *Store) Refresh(ctx context.Context, in RefreshInput) (*RefreshResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if in.RunID == "" {
		return nil, camperrors.NewValidation("run_id", "is required", nil)
	}
	if in.Actor == "" {
		return nil, camperrors.NewValidation("actor", "is required", nil)
	}

	// Anchors resolve against the whole campaign, not the run's scope: a
	// scoped run's evidence may well anchor a festival or workitem the scope
	// excludes, and failing to observe it would read as a change.
	anchorIndex := IndexDiscovery(in.Items)

	result := &RefreshResult{RunID: in.RunID}
	manifestPath := filepath.Join(s.RunDir(in.RunID), ManifestFileName)

	err := withLock(ctx, lockPathFor(manifestPath), func() error {
		run, err := s.OpenRun(ctx, in.RunID)
		if err != nil {
			return err
		}

		// Membership — what is gone, what is new — is answered inside the
		// run's own scope, rebuilt from the manifest so it matches the
		// selection `start` froze rather than whatever the profile says now.
		scoped, err := scopedItems(run.Manifest, in.Items)
		if err != nil {
			return err
		}
		discovered := IndexDiscovery(scoped).ByStableID
		byWorkItem := indexWorkItems(scoped)

		anchors, err := s.AnchorsByRow(ctx, in.RunID, rowIDs(run.Manifest.Rows))
		if err != nil {
			return err
		}
		// Campaign root, not the store root: a path anchor is
		// campaign-relative, so resolving it under .campaign/triage would
		// hash nothing and report every anchored row as changed.
		checks, err := CheckLocalAnchors(ctx, s.campaignRoot, anchors, anchorIndex)
		if err != nil {
			return err
		}

		result.Diff = ClassifyRows(DiffInput{
			Rows:       run.Manifest.Rows,
			Discovered: discovered,
			Anchors:    checks,
		})

		verdicts, err := s.Verdicts(ctx, in.RunID)
		if err != nil {
			return err
		}

		changed, err := s.applyDiffEffects(ctx, in, run.Manifest, result, verdicts, byWorkItem)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		body, err := MarshalDocument(run.Manifest)
		if err != nil {
			return err
		}
		return writeAtomic(manifestPath, body)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// applyDiffEffects records stale events, re-keys moved rows, and appends new
// ones. It reports whether the manifest needs rewriting.
//
// Stale events are appended before the manifest is written. If the process
// dies between the two, the run re-reads as "verdict stale, row not yet
// re-keyed", and the next refresh reclassifies and converges. The other order
// would leave a re-keyed row still carrying a verdict this pass decided was
// invalid, which is the failure that actually costs something.
func (s *Store) applyDiffEffects(
	ctx context.Context,
	in RefreshInput,
	manifest *Manifest,
	result *RefreshResult,
	verdicts map[string]RowVerdict,
	byWorkItem map[string]workitem.WorkItem,
) (bool, error) {
	rowAt := indexRows(manifest.Rows)
	changed := false

	for _, diff := range result.Diff.Rows {
		switch diff.Class {
		case ClassChanged, ClassGone:
			recorded, err := s.recordStale(ctx, in, diff, verdicts[diff.StableID])
			if err != nil {
				return false, err
			}
			if recorded {
				result.StaleRecorded = append(result.StaleRecorded, diff.StableID)
			}

		case ClassMoved:
			i, ok := rowAt[diff.StableID]
			if !ok || diff.Moved == nil {
				continue
			}
			rekeyRow(&manifest.Rows[i], *diff.Moved)
			result.Rekeyed = append(result.Rekeyed, diff.StableID)
			changed = true

		case ClassNew:
			item, ok := byWorkItem[diff.StableID]
			if !ok {
				continue
			}
			row := rowFor(item, manifest.Profile.Resolved, in.Now)
			row.Batch = nextBatchFor(manifest, len(result.Appended))
			manifest.Rows = append(manifest.Rows, row)
			result.Appended = append(result.Appended, diff.StableID)
			changed = true
		}
	}
	return changed, nil
}

// recordStale appends a stale event when the row still holds a live verdict,
// and reports whether it did.
//
// A row whose verdict is already rejected, superseded, or stale has nothing
// left to retire, and a row that was never judged has nothing to retire
// either. Appending anyway would put events in the stream that assert no fact,
// and `status` would report verdicts going stale that no one ever held.
func (s *Store) recordStale(ctx context.Context, in RefreshInput, diff RowDiff, verdict RowVerdict) (bool, error) {
	if !HasLiveProposal(verdict) {
		return false, nil
	}
	event := DecisionEvent{
		Event:    DecisionStale,
		StableID: diff.StableID,
		Actor:    in.Actor,
		At:       in.Now,
		Note:     string(diff.Class) + ": " + diff.Reason,
	}
	if err := s.AppendDecision(ctx, in.RunID, event); err != nil {
		return false, err
	}
	return true, nil
}

// rekeyRow moves a manifest row to where the item now lives, leaving identity,
// policy, batch, and carry provenance alone. Those are what the verdict was
// formed against; the location is the only thing that moved.
//
// A path-bound row never reaches here: classifyRow sends a moved row with an
// identity exception to `changed` instead, because path is the only identity
// it has and the move invalidated it.
func rekeyRow(row *ManifestRow, move Relocation) {
	row.Key = move.ToKey
	row.RelativePath = move.ToRelativePath
	row.LifecycleStage = move.ToLifecycleStage
	row.AttentionStage = move.ToAttentionStage
}

// nextBatchFor numbers appended rows into batches after the manifest's last
// one, chunked by the run's frozen batch size.
//
// New rows are appended rather than merged into the existing (type, key)
// order on purpose: re-sorting would renumber batches under a reviewer who is
// part-way through the run. Determinism survives because the appended set is
// itself emitted in sorted order.
func nextBatchFor(manifest *Manifest, appendedSoFar int) int {
	size := manifest.Profile.Resolved.Review.BatchSize
	if size < 1 {
		size = 1
	}
	return BatchCount(manifest) + 1 + appendedSoFar/size
}

// indexRows maps stable id to position in the manifest.
func indexRows(rows []ManifestRow) map[string]int {
	out := make(map[string]int, len(rows))
	for i, row := range rows {
		out[row.StableID] = i
	}
	return out
}

// rowIDs lists the stable ids of a manifest's rows.
func rowIDs(rows []ManifestRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.StableID)
	}
	return out
}

// scopedItems narrows a full campaign walk to the run's frozen scope: the
// profile embedded in the manifest, plus the `--scope` expressions the run was
// started with.
//
// Both halves are needed. The embedded profile carries scope.exclude_types and
// friends; the expressions carry what the operator typed. Rebuilding from the
// live profile instead would let an unrelated profile edit widen a run that is
// already under review.
func scopedItems(manifest *Manifest, items []workitem.WorkItem) ([]workitem.WorkItem, error) {
	scope := NewScope(manifest.Profile.Resolved)
	if err := scope.ApplyExpressions(manifest.ScopeExpressions); err != nil {
		return nil, camperrors.Wrap(err, "reapplying the run's frozen scope")
	}
	return scope.Apply(items), nil
}

// indexWorkItems keys a discovery walk by the same identity manifest rows use,
// so a new row can be built from the full item rather than the diff's summary.
func indexWorkItems(items []workitem.WorkItem) map[string]workitem.WorkItem {
	out := make(map[string]workitem.WorkItem, len(items))
	for _, item := range items {
		if workitem.InDungeonPath(item.RelativePath) {
			continue
		}
		out[StableIDFor(item)] = item
	}
	return out
}
