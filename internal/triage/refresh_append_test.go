package triage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

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
