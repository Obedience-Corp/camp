package triage

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- concurrency -------------------------------------------------------

// TestCreateRunIsSerialized: checking for an active run and creating one are a
// read-modify-write over the same state. Without one lock around both, two
// starts can each observe no active run and each create one, defeating the
// check entirely.
func TestCreateRunIsSerialized(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	const racers = 4
	var wg sync.WaitGroup
	results := make([]error, racers)
	created := make([]string, racers)
	start := make(chan struct{})

	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			run, err := store.CreateRun(ctx, newManifestForStore())
			results[i] = err
			if run != nil {
				created[i] = run.ID
			}
		}()
	}
	close(start)
	wg.Wait()

	succeeded := 0
	for i, err := range results {
		if err == nil {
			succeeded++
			continue
		}
		assert.True(t,
			camperrors.Is(err, camperrors.ErrConflict) || camperrors.Is(err, camperrors.ErrAlreadyExists),
			"racer %d should lose cleanly, got: %v", i, err)
	}
	assert.Equal(t, 1, succeeded, "exactly one concurrent start may create a run")

	entries, err := os.ReadDir(filepath.Join(store.Root(), RunsDirName))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "only the winner's run directory should exist")
}

// TestSetPhaseIsSerialized: the phase history is what makes a killed run
// resumable, so two writers must not each append a transition to the state
// they separately read and drop one of the two.
func TestSetPhaseIsSerialized(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	const racers = 4
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = store.SetPhase(ctx, run.ID, PhaseSnapshotted, "concurrent")
		}()
	}
	close(start)
	wg.Wait()

	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, PhaseSnapshotted, reopened.State.Phase)
	assert.Len(t, reopened.State.PhaseHistory, 2,
		"the transition must be recorded exactly once")
	assert.Empty(t, reopened.State.Validate(), "state must stay a legal walk")
}

// TestAppendDecisionIsSerialized: concurrent appends must all land as whole
// lines, and a reader must never see a torn one.
func TestAppendDecisionIsSerialized(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	const racers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			e := validDecision()
			e.StableID = "row-" + strconv.Itoa(i)
			require.NoError(t, store.AppendDecision(ctx, run.ID, *e))
		}()
	}
	close(start)
	wg.Wait()

	events, err := store.Decisions(ctx, run.ID)

	require.NoError(t, err, "every line must parse")
	assert.Len(t, events, racers)
	assert.Len(t, FoldDecisions(events), racers, "no row may be lost or duplicated")
}
