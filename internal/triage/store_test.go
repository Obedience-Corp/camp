package triage

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStore returns a store over a scratch campaign with a clock that
// advances one second per call, so run ids and phase timestamps are
// deterministic and ordered without reading the wall clock.
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	// Guarded because the concurrency tests call through the clock from
	// several goroutines; a racy test clock would report the store as broken.
	var mu sync.Mutex
	tick := 0
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		tick++
		return testAt.Add(time.Duration(tick) * time.Second)
	}
	return NewStore(root, clock), root
}

func newManifestForStore() *Manifest {
	m := validManifest()
	m.RunID = ""
	m.CreatedAt = time.Time{}
	return m
}

// --- error cases first -------------------------------------------------

// TestCreateRunRefusesWhileAnotherIsActive: two live runs would each hold a
// snapshot of the same campaign and race each other's verdicts.
func TestCreateRunRefusesWhileAnotherIsActive(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	first, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	_, err = store.CreateRun(ctx, newManifestForStore())

	require.Error(t, err)
	assert.True(t, camperrors.Is(err, camperrors.ErrConflict))
	assert.Contains(t, err.Error(), first.ID)
	assert.Contains(t, err.Error(), "camp triage abandon")
}

// TestCreateRunAllowsAnotherAfterAbandon is the other half: closing a run
// explicitly is what unblocks the next one.
func TestCreateRunAllowsAnotherAfterAbandon(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	first, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)
	_, err = store.Abandon(ctx, first.ID, "scope was wrong")
	require.NoError(t, err)

	second, err := store.CreateRun(ctx, newManifestForStore())

	require.NoError(t, err)
	assert.NotEqual(t, first.ID, second.ID)

	latest, err := store.LatestRunID(ctx)
	require.NoError(t, err)
	assert.Equal(t, second.ID, latest)
}

// TestCreateRunRejectsInvalidManifestWithoutTouchingDisk: an invalid manifest
// must not leave a half-built run directory behind for the next start to trip
// over.
func TestCreateRunRejectsInvalidManifestWithoutTouchingDisk(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	manifest := newManifestForStore()
	manifest.Rows[0].Batch = 0

	_, err := store.CreateRun(ctx, manifest)

	require.Error(t, err)
	require.ErrorIs(t, err, camperrors.ErrInvalidInput)

	entries, readErr := os.ReadDir(filepath.Join(store.Root(), RunsDirName))
	assert.True(t, os.IsNotExist(readErr) || len(entries) == 0,
		"no run directory should exist after a rejected manifest")
	_, err = store.LatestRunID(ctx)
	assert.Error(t, err, "latest must not point at a run that was never written")
}

// TestOpenRunMissing reports a not-found rather than an empty run.
func TestOpenRunMissing(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	_, err := store.OpenRun(ctx, "run-19700101T000000Z")

	require.Error(t, err)
	var notFound *camperrors.NotFoundError
	assert.True(t, camperrors.As(err, &notFound))
}

// TestOpenLatestBeforeAnyRun: a campaign that has never triaged reports
// not-found, which is what `status` distinguishes from a broken store.
func TestOpenLatestBeforeAnyRun(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	_, err := store.OpenLatest(ctx)

	var notFound *camperrors.NotFoundError
	assert.True(t, camperrors.As(err, &notFound))
}

// TestSetPhaseRejectsIllegalTransition keeps the machine honest at the write
// boundary, not only in validation.
func TestSetPhaseRejectsIllegalTransition(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	_, err = store.SetPhase(ctx, run.ID, PhaseVerified, "")

	require.Error(t, err)
	require.ErrorIs(t, err, camperrors.ErrInvalidInput)

	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, PhaseCreated, reopened.State.Phase, "a refused transition changes nothing")
}

// TestAbandonRejectsClosedRun: a verified run is finished; reopening it as
// abandoned would rewrite history.
func TestAbandonRejectsClosedRun(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run := advanceTo(t, store, PhaseVerified)

	_, err := store.Abandon(ctx, run.ID, "changed my mind")

	require.Error(t, err)
	require.ErrorIs(t, err, camperrors.ErrInvalidInput)
}

// TestDecisionsRejectsCorruptStream: a truncated or hand-edited line is
// reported with its line number rather than silently skipped.
func TestDecisionsRejectsCorruptStream(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)
	require.NoError(t, store.AppendDecision(ctx, run.ID, *validDecision()))

	path := filepath.Join(run.Dir, DecisionsFileName)
	require.NoError(t, appendRaw(path, "{\"schema_version\":\"triage/v1alpha1\",\"event\":\"invented\"}\n"))

	_, err = store.Decisions(ctx, run.ID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 2")
}

// TestStoreOperationsRespectContextCancellation: every entry point checks the
// context before touching the filesystem.
func TestStoreOperationsRespectContextCancellation(t *testing.T) {
	store, _ := newTestStore(t)
	live, err := store.CreateRun(context.Background(), newManifestForStore())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		call func() error
	}{
		{"create", func() error { _, err := store.CreateRun(ctx, newManifestForStore()); return err }},
		{"open", func() error { _, err := store.OpenRun(ctx, live.ID); return err }},
		{"open latest", func() error { _, err := store.OpenLatest(ctx); return err }},
		{"set phase", func() error { _, err := store.SetPhase(ctx, live.ID, PhaseSnapshotted, ""); return err }},
		{"abandon", func() error { _, err := store.Abandon(ctx, live.ID, ""); return err }},
		{"append decision", func() error { return store.AppendDecision(ctx, live.ID, *validDecision()) }},
		{"decisions", func() error { _, err := store.Decisions(ctx, live.ID); return err }},
		{"write evidence", func() error { _, err := store.WriteEvidence(ctx, live.ID, validEvidence()); return err }},
		{"evidence", func() error { _, err := store.Evidence(ctx, live.ID, "x"); return err }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, tc.call(), context.Canceled)
		})
	}
}

// --- concurrency -------------------------------------------------------

// TestCreateRunIsSerialized: checking for an active run and creating one are a
// read-modify-write over the same state. Without one lock around both, two
// starts can each observe no active run and each create one, defeating the
// check entirely.
func TestCreateRunIsSerialized(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	const racers = 8
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

	const racers = 8
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

	const racers = 16
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

// --- lifecycle ---------------------------------------------------------

// TestCreateRunWritesACompleteRun pins the on-disk shape later sequences read.
func TestCreateRunWritesACompleteRun(t *testing.T) {
	ctx := context.Background()
	store, root := newTestStore(t)

	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	assert.Equal(t, "run-20260810T140001Z", run.ID, "run id comes from the injected clock")
	assert.Equal(t, PhaseCreated, run.State.Phase)
	assert.Equal(t, run.ID, run.Manifest.RunID)

	for _, rel := range []string{ManifestFileName, RunStateFileName, EvidenceDirName} {
		_, err := os.Stat(filepath.Join(run.Dir, rel))
		assert.NoError(t, err, "run should contain %s", rel)
	}
	latest, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(DirName), LatestFileName))
	require.NoError(t, err)
	assert.Equal(t, run.ID+"\n", string(latest))
}

// TestRunSurvivesReopen: everything a later command needs comes back off disk
// unchanged, which is what makes a run resumable across processes.
func TestRunSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	store, root := newTestStore(t)
	created, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	// A different Store instance, as a later camp invocation would be.
	reopened, err := NewStore(root, nil).OpenLatest(ctx)

	require.NoError(t, err)
	assert.Equal(t, created.ID, reopened.ID)
	assert.Equal(t, created.Manifest.Rows, reopened.Manifest.Rows)
	assert.Equal(t, created.Manifest.Profile.Resolved, reopened.Manifest.Profile.Resolved)
	assert.True(t, reopened.Active())
}

// TestSetPhaseRecordsHistoryBeforeSideEffects is the resume contract, tested
// at the instant it matters: the failpoint fires after the state write, so the
// call fails exactly where a kill would land. Reopening must find the new
// phase, because the phase's work may be half-done and has to be redone.
func TestSetPhaseRecordsHistoryBeforeSideEffects(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	t.Setenv("CAMP_TEST_FAILPOINT", SiteSetPhaseAfterStateWrite+"=error")
	_, err = store.SetPhase(ctx, run.ID, PhaseSnapshotted, "snapshotting")
	require.Error(t, err, "the failpoint stands in for the process dying mid-phase")
	require.Contains(t, err.Error(), SiteSetPhaseAfterStateWrite,
		"the failure must come from the injected failpoint, not from an earlier step")

	resumed, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, PhaseSnapshotted, resumed.State.Phase)
	require.Len(t, resumed.State.PhaseHistory, 2)
	assert.Equal(t, PhaseSnapshotted, resumed.State.PhaseHistory[1].Phase)
	assert.Equal(t, "snapshotting", resumed.State.PhaseHistory[1].Note)
}

// TestSetPhaseIsIdempotent: re-entering the phase you are already in is a
// no-op, so a resumed process does not double-record its own transition.
func TestSetPhaseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	_, err = store.SetPhase(ctx, run.ID, PhaseSnapshotted, "first")
	require.NoError(t, err)
	_, err = store.SetPhase(ctx, run.ID, PhaseSnapshotted, "again")
	require.NoError(t, err)

	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Len(t, reopened.State.PhaseHistory, 2)
}

// TestAbandonKeepsState: an abandoned run is still the base an incremental
// run diffs against, so nothing is deleted and latest still points at it.
func TestAbandonKeepsState(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)
	require.NoError(t, store.AppendDecision(ctx, run.ID, *validDecision()))

	abandoned, err := store.Abandon(ctx, run.ID, "scope was wrong")
	require.NoError(t, err)

	assert.Equal(t, PhaseAbandoned, abandoned.State.Phase)
	require.NotNil(t, abandoned.State.AbandonReason)
	assert.Equal(t, "scope was wrong", *abandoned.State.AbandonReason)
	assert.False(t, abandoned.Active())

	latest, err := store.OpenLatest(ctx)
	require.NoError(t, err)
	assert.Equal(t, run.ID, latest.ID)

	events, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	assert.Len(t, events, 1, "abandoning keeps recorded judgment")
}

// TestAbandonWithoutReason leaves the field unset rather than storing "".
func TestAbandonWithoutReason(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	abandoned, err := store.Abandon(ctx, run.ID, "   ")

	require.NoError(t, err)
	assert.Nil(t, abandoned.State.AbandonReason)
	assert.Empty(t, abandoned.State.Validate())
}

// --- helpers -----------------------------------------------------------

// advanceTo walks a fresh run forward to the requested phase.
func advanceTo(t *testing.T, store *Store, phase Phase) *Run {
	t.Helper()
	ctx := context.Background()
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	for _, step := range []Phase{PhaseSnapshotted, PhaseJudging, PhaseReviewing, PhaseApplying, PhaseVerified} {
		run, err = store.SetPhase(ctx, run.ID, step, "")
		require.NoError(t, err)
		if step == phase {
			return run
		}
	}
	return run
}

// appendRaw writes a line straight to a stream, standing in for a hand edit.
func appendRaw(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(line)
	return err
}
