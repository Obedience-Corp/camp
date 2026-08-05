package triage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

const (
	hubID    = "design-festival-hub-control-plane-2026-08-04"
	legacyID = "legacy-design-no-marker"
)

// refreshStore is a run whose two rows both carry an approved verdict, which
// is the state refresh exists to protect.
func refreshStore(t *testing.T) (*Store, string, *Run) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store := NewStore(root, func() time.Time { return testAt })

	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	for i, row := range run.Manifest.Rows {
		record := validEvidence()
		record.StableID = row.StableID
		record.Anchors = nil
		_, err := store.WriteEvidence(ctx, run.ID, record)
		require.NoError(t, err)

		disposition := []string{"parked", "completed"}[i%2]
		_, err = store.Propose(ctx, ProposeInput{
			RunID: run.ID, StableID: row.StableID,
			Disposition: disposition, Rationale: rationale("because"),
			Actor: "tester", Now: testAt,
		})
		require.NoError(t, err)
	}
	return store, root, run
}

// refreshItems re-discovers both manifest rows exactly where they were.
func refreshItems() []workitem.WorkItem {
	return []workitem.WorkItem{
		{
			StableID: hubID, WorkflowType: "design",
			Key:            "design:workflow/design/festival-hub-control-plane",
			Title:          "Festival hub control plane",
			RelativePath:   "workflow/design/festival-hub-control-plane",
			LifecycleStage: "active", AttentionStage: "active",
		},
		{
			StableID: legacyID, WorkflowType: "design",
			Key:          "design:workflow/design/legacy",
			Title:        "Legacy design",
			RelativePath: "workflow/design/legacy",
		},
	}
}

func refresh(t *testing.T, store *Store, runID string, items []workitem.WorkItem) *RefreshResult {
	t.Helper()
	result, err := store.Refresh(context.Background(), RefreshInput{
		RunID: runID, Items: items, Actor: "tester", Now: testAt,
	})
	require.NoError(t, err)
	return result
}

// classOf reads one row's class out of a result.
func classOf(t *testing.T, result *RefreshResult, stableID string) RowClass {
	t.Helper()
	for _, row := range result.Diff.Rows {
		if row.StableID == stableID {
			return row.Class
		}
	}
	t.Fatalf("no row %q in the diff", stableID)
	return ""
}

// TestRefreshLeavesAnUnchangedRunAlone is the baseline: refreshing a campaign
// where nothing moved must record nothing at all.
func TestRefreshLeavesAnUnchangedRunAlone(t *testing.T) {
	ctx := context.Background()
	store, _, run := refreshStore(t)

	before, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)

	result := refresh(t, store, run.ID, refreshItems())

	assert.Equal(t, ClassFresh, classOf(t, result, hubID))
	assert.Equal(t, ClassFresh, classOf(t, result, legacyID))
	assert.Empty(t, result.StaleRecorded)
	assert.Empty(t, result.Rekeyed)
	assert.Empty(t, result.Appended)

	after, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a no-op refresh appends no events")
}

// TestRefreshRekeysAMovedRowAndKeepsItsVerdict is FT-006 end to end through
// the store: the row moved, the manifest follows it, and the verdict survives.
func TestRefreshRekeysAMovedRowAndKeepsItsVerdict(t *testing.T) {
	ctx := context.Background()
	store, _, run := refreshStore(t)

	items := refreshItems()
	items[0].RelativePath = "workflow/design/moved-hub"
	items[0].Key = "design:workflow/design/moved-hub"
	items[0].AttentionStage = "parked"

	result := refresh(t, store, run.ID, items)

	assert.Equal(t, ClassMoved, classOf(t, result, hubID))
	assert.Equal(t, []string{hubID}, result.Rekeyed)
	assert.Empty(t, result.StaleRecorded, "a move never retires a verdict")

	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	row := rowByID(t, reopened.Manifest, hubID)
	assert.Equal(t, "workflow/design/moved-hub", row.RelativePath)
	assert.Equal(t, "design:workflow/design/moved-hub", row.Key)
	assert.Equal(t, "parked", row.AttentionStage)
	assert.Equal(t, 2, row.Batch, "a re-key must not renumber the review batch")

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, VerdictProposed, verdicts[hubID].State)
	assert.Equal(t, "parked", verdicts[hubID].Disposition)

	// The move is reported once. A second refresh sees the re-keyed manifest.
	again := refresh(t, store, run.ID, items)
	assert.Equal(t, ClassFresh, classOf(t, again, hubID))
	assert.Empty(t, again.Rekeyed)
}

// TestRefreshRetiresAVerdictWhoseAnchorMoved is the changed path: a real
// anchor file is edited between the proposal and the refresh.
func TestRefreshRetiresAVerdictWhoseAnchorMoved(t *testing.T) {
	ctx := context.Background()
	store, root, run := refreshStore(t)

	hash := writeAnchorFile(t, root, "docs/anchored.md", "as judged\n")
	record := validEvidence()
	record.StableID = hubID
	record.Anchors = []Anchor{{Kind: AnchorKindPath, Path: "docs/anchored.md", Hash: hash}}
	_, err := store.WriteEvidence(ctx, run.ID, record)
	require.NoError(t, err)

	// Nothing moved yet.
	assert.Equal(t, ClassFresh, classOf(t, refresh(t, store, run.ID, refreshItems()), hubID))

	writeAnchorFile(t, root, "docs/anchored.md", "edited after the verdict\n")
	result := refresh(t, store, run.ID, refreshItems())

	assert.Equal(t, ClassChanged, classOf(t, result, hubID))
	assert.Equal(t, []string{hubID}, result.StaleRecorded)

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, VerdictStale, verdicts[hubID].State)
	assert.False(t, verdicts[hubID].Applicable(), "a stale verdict must not apply")
	assert.Equal(t, "parked", verdicts[hubID].Disposition,
		"the retired disposition stays visible so status can say what went stale")

	events, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	last := events[len(events)-1]
	assert.Equal(t, DecisionStale, last.Event)
	assert.Equal(t, "tester", last.Actor)
	assert.Contains(t, last.Note, "changed:", "the event carries its own reason")
	assert.Contains(t, last.Note, "path:docs/anchored.md")

	assert.False(t, HasLiveProposal(verdicts[hubID]))
}

// TestRefreshRetiresAGoneRow covers the FT-006 external-completion story.
func TestRefreshRetiresAGoneRow(t *testing.T) {
	ctx := context.Background()
	store, _, run := refreshStore(t)

	result := refresh(t, store, run.ID, refreshItems()[:1])

	assert.Equal(t, ClassGone, classOf(t, result, legacyID))
	assert.Equal(t, []string{legacyID}, result.StaleRecorded)

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, VerdictStale, verdicts[legacyID].State)

	events, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	assert.Contains(t, events[len(events)-1].Note, "external completion")
}

// TestRefreshDoesNotRetireAVerdictThatIsNotLive: a row nobody judged, or whose
// proposal was already rejected, has nothing to retire. Appending anyway would
// make status report verdicts going stale that no one ever held.
func TestRefreshDoesNotRetireAVerdictThatIsNotLive(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := NewStore(root, func() time.Time { return testAt })

	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	// No proposals at all: both rows are unjudged.
	result := refresh(t, store, run.ID, nil)

	assert.Equal(t, ClassGone, classOf(t, result, hubID))
	assert.Equal(t, ClassGone, classOf(t, result, legacyID))
	assert.Empty(t, result.StaleRecorded,
		"an unjudged row is classified, not retired")

	events, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	assert.Empty(t, events)
}

// TestRefreshAppendsANewDiscovery covers the fifth class through the store,
// including the batch numbering that keeps an in-flight review stable.
func TestRefreshAppendsANewDiscovery(t *testing.T) {
	ctx := context.Background()
	store, _, run := refreshStore(t)

	before, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	highestBefore := BatchCount(before.Manifest)

	items := append(refreshItems(), workitem.WorkItem{
		StableID: "design-brand-new", WorkflowType: "design",
		Key:            "design:workflow/design/brand-new",
		Title:          "Brand new design",
		RelativePath:   "workflow/design/brand-new",
		AttentionStage: "active",
	})

	result := refresh(t, store, run.ID, items)

	assert.Equal(t, ClassNew, classOf(t, result, "design-brand-new"))
	assert.Equal(t, []string{"design-brand-new"}, result.Appended)

	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	row := rowByID(t, reopened.Manifest, "design-brand-new")
	assert.Equal(t, "workflow/design/brand-new", row.RelativePath)
	assert.Equal(t, "design", row.Type)
	assert.Greater(t, row.Batch, highestBefore,
		"a new row lands in a fresh batch rather than renumbering the run")
	assert.NotEmpty(t, row.Policy.Evidence, "the row gets the run's frozen policy")

	// The existing rows keep the batch numbers a reviewer may already be
	// working through.
	assert.Equal(t, 2, rowByID(t, reopened.Manifest, hubID).Batch)

	// Appending is not repeated on the next pass.
	again := refresh(t, store, run.ID, items)
	assert.Empty(t, again.Appended)
	assert.Equal(t, ClassFresh, classOf(t, again, "design-brand-new"))
	assert.Len(t, reopened.Manifest.Rows, 3)
}

// TestRefreshHonorsTheRunsFrozenScope is the bug the scope_expressions field
// exists to prevent: without it, every out-of-scope item in the campaign looks
// like a new discovery and gets appended to the manifest.
func TestRefreshHonorsTheRunsFrozenScope(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := NewStore(root, func() time.Time { return testAt })

	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	manifest.ScopeExpressions = []string{"type:design"}
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	items := append(refreshItems(), workitem.WorkItem{
		SourceID: "CI0042", WorkflowType: "intent",
		Key:          "intent:.campaign/intents/inbox/unrelated.md",
		RelativePath: ".campaign/intents/inbox/unrelated.md",
	})

	result := refresh(t, store, run.ID, items)

	assert.Empty(t, result.Appended,
		"an intent is outside a type:design run and is not a new discovery")
	assert.Equal(t, 0, result.Diff.CountByClass()[ClassNew])

	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Len(t, reopened.Manifest.Rows, 2)
}

// TestRefreshResolvesAnchorsOutsideTheRunsScope is the converse: a scoped run
// may anchor evidence to something the scope excludes, and failing to observe
// it would read as a change and retire a perfectly good verdict.
func TestRefreshResolvesAnchorsOutsideTheRunsScope(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := NewStore(root, func() time.Time { return testAt })

	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	manifest.ScopeExpressions = []string{"type:design"}
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	record := validEvidence()
	record.StableID = hubID
	record.Anchors = []Anchor{{Kind: AnchorKindFestival, ID: "CI0009", Observed: "active"}}
	_, err = store.WriteEvidence(ctx, run.ID, record)
	require.NoError(t, err)

	_, err = store.Propose(ctx, ProposeInput{
		RunID: run.ID, StableID: hubID, Disposition: "parked",
		Rationale: rationale("because"), Actor: "tester", Now: testAt,
	})
	require.NoError(t, err)

	// The festival is a different type, so the run's scope excludes it.
	items := append(refreshItems(), workitem.WorkItem{
		SourceID: "CI0009", WorkflowType: workitem.WorkflowTypeFestival,
		Key:            "festival:festivals/active/CI0009",
		RelativePath:   "festivals/active/CI0009",
		LifecycleStage: "active",
	})

	result := refresh(t, store, run.ID, items)

	assert.Equal(t, ClassFresh, classOf(t, result, hubID),
		"an anchor outside the run's scope still resolves; scope decides membership, not observation")
	assert.Empty(t, result.StaleRecorded)
}

// TestRefreshRequiresAnActor keeps unattributed events out of the stream.
func TestRefreshRequiresAnActor(t *testing.T) {
	store, _, run := refreshStore(t)
	_, err := store.Refresh(context.Background(), RefreshInput{
		RunID: run.ID, Items: refreshItems(), Now: testAt,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "actor")
}

// TestRefreshRejectsAMissingRunID covers the other required input.
func TestRefreshRejectsAMissingRunID(t *testing.T) {
	store, _, _ := refreshStore(t)
	_, err := store.Refresh(context.Background(), RefreshInput{
		Items: refreshItems(), Actor: "tester", Now: testAt,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run_id")
}

// TestRefreshHonorsCancellation is the context rule at the store boundary.
func TestRefreshHonorsCancellation(t *testing.T) {
	store, _, run := refreshStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.Refresh(ctx, RefreshInput{
		RunID: run.ID, Items: refreshItems(), Actor: "tester", Now: testAt,
	})
	assert.ErrorIs(t, err, context.Canceled)
}

// TestRefreshIsIdempotent runs the same drift twice and asserts the second
// pass changes nothing — the property a resumable command depends on.
func TestRefreshIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store, _, run := refreshStore(t)

	items := refreshItems()
	items[0].RelativePath = "workflow/design/moved-hub"
	items[0].Key = "design:workflow/design/moved-hub"
	items = items[:1] // and the legacy row is gone

	first := refresh(t, store, run.ID, items)
	assert.Equal(t, []string{hubID}, first.Rekeyed)
	assert.Equal(t, []string{legacyID}, first.StaleRecorded)

	afterFirst, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	manifestAfterFirst, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)

	second := refresh(t, store, run.ID, items)
	assert.Empty(t, second.Rekeyed)
	assert.Empty(t, second.StaleRecorded,
		"the gone row's verdict was already retired; there is nothing left to retire")
	assert.Equal(t, ClassFresh, classOf(t, second, hubID))
	assert.Equal(t, ClassGone, classOf(t, second, legacyID),
		"a gone row keeps reporting gone; it is the event that is not repeated")

	afterSecond, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, afterFirst, afterSecond)

	manifestAfterSecond, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, manifestAfterFirst.Manifest.Rows, manifestAfterSecond.Manifest.Rows)
}

// TestRefreshCountsUncheckedAnchors covers the offline reporting requirement:
// a row whose only anchor could not be checked is fresh, and said to be
// unchecked rather than quietly counted as verified.
func TestRefreshCountsUncheckedAnchors(t *testing.T) {
	ctx := context.Background()
	store, _, run := refreshStore(t)

	record := validEvidence()
	record.StableID = hubID
	record.Anchors = []Anchor{{
		Kind: AnchorKindPR, Repo: "Obedience-Corp/camp", Number: 546, Observed: "open",
	}}
	_, err := store.WriteEvidence(ctx, run.ID, record)
	require.NoError(t, err)

	result := refresh(t, store, run.ID, refreshItems())

	assert.Equal(t, ClassFresh, classOf(t, result, hubID))
	assert.Equal(t, 1, result.RowsWithUncheckedAnchors())
	assert.Empty(t, result.StaleRecorded,
		"offline must not invalidate a run")

	for _, row := range result.Diff.Rows {
		if row.StableID == hubID {
			assert.Equal(t, 1, row.UncheckedAnchors)
			assert.Contains(t, row.Reason, "unchecked")
		}
	}
}

// rowByID finds a manifest row or fails the test.
func rowByID(t *testing.T, manifest *Manifest, stableID string) ManifestRow {
	t.Helper()
	for _, row := range manifest.Rows {
		if row.StableID == stableID {
			return row
		}
	}
	t.Fatalf("no row %q in the manifest", stableID)
	return ManifestRow{}
}

// TestRefreshFillsBatchesWhenAppendingSeveralRows is a regression guard found
// in review: nextBatchFor read the manifest's batch count, which the append
// loop had already raised, so every new row claimed a batch of its own and
// the run's batch size was ignored.
func TestRefreshFillsBatchesWhenAppendingSeveralRows(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := NewStore(root, func() time.Time { return testAt })

	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	size := run.Manifest.Profile.Resolved.Review.BatchSize
	require.Equal(t, 5, size, "the fixture's batch size is what makes this meaningful")
	base := BatchCount(run.Manifest)

	items := refreshItems()
	for _, suffix := range []string{"a", "b", "c", "d", "e", "f"} {
		items = append(items, workitem.WorkItem{
			StableID: "design-new-" + suffix, WorkflowType: "design",
			Key:          "design:workflow/design/new-" + suffix,
			RelativePath: "workflow/design/new-" + suffix,
		})
	}

	result := refresh(t, store, run.ID, items)
	require.Len(t, result.Appended, 6)

	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)

	// Six rows at a batch size of five: five in the first new batch, one in
	// the next. Not six batches.
	byBatch := map[int]int{}
	for _, suffix := range []string{"a", "b", "c", "d", "e", "f"} {
		byBatch[rowByID(t, reopened.Manifest, "design-new-"+suffix).Batch]++
	}
	assert.Equal(t, map[int]int{base + 1: 5, base + 2: 1}, byBatch)

	// And the pre-existing rows keep the batch numbers a reviewer may already
	// be working through.
	assert.Equal(t, 2, rowByID(t, reopened.Manifest, hubID).Batch)
	assert.Equal(t, 1, rowByID(t, reopened.Manifest, legacyID).Batch)
}
