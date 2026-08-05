package triage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil/failpoint"
)

// Layout under the campaign root. Everything a run needs is inside its own
// directory, so a run can be read, diffed, or archived as a unit.
const (
	// DirName is the triage tree, relative to the campaign root.
	DirName = ".campaign/triage"
	// RunsDirName holds one directory per run.
	RunsDirName = "runs"
	// LatestFileName points at the most recent run.
	LatestFileName = "latest"

	ManifestFileName     = "manifest.json"
	RunStateFileName     = "run.json"
	DecisionsFileName    = "decisions.jsonl"
	EvidenceDirName      = "evidence"
	ApplyPlanFileName    = "apply-plan.json"
	ReceiptsFileName     = "receipts.jsonl"
	VerificationFileName = "verification.json"
)

const (
	fileMode = 0o644
	dirMode  = 0o755
	// createLockName guards the whole start-a-run sequence. It is its own
	// lock rather than the `latest` pointer's, because creating a run writes
	// that pointer and a non-reentrant lock cannot be taken twice.
	createLockName = "create.lock"
)

// SiteSetPhaseAfterStateWrite is the failpoint between recording a phase and
// beginning that phase's side effects. It exists so the resume guarantee can
// be tested at the exact instant it matters rather than asserted in prose.
const SiteSetPhaseAfterStateWrite = "triage.set_phase.after_state_write"

// Clock supplies the time the store stamps into run ids and phase history.
// It is a dependency rather than a global so a run id is reproducible in a
// test and a run's timestamps are whatever the caller decided they are.
type Clock func() time.Time

// SystemClock is the production clock.
func SystemClock() time.Time { return time.Now() }

// Store reads and writes triage runs under <campaign>/.campaign/triage.
//
// Every write is atomic and holds a per-file lock, following the same
// discipline as internal/workitem/promotion.go: a concurrent camp invocation
// either sees the previous content or the new content, never a partial file.
type Store struct {
	root  string
	clock Clock
}

// NewStore returns a Store rooted at campaignRoot. A nil clock means the
// system clock.
func NewStore(campaignRoot string, clock Clock) *Store {
	if clock == nil {
		clock = SystemClock
	}
	return &Store{
		root:  filepath.Join(campaignRoot, filepath.FromSlash(DirName)),
		clock: clock,
	}
}

// Root is the absolute path of the triage tree.
func (s *Store) Root() string { return s.root }

// RunDir is the absolute path of one run's directory.
func (s *Store) RunDir(runID string) string {
	return filepath.Join(s.root, RunsDirName, runID)
}

// Run is an opened run: its frozen snapshot and its recorded position.
type Run struct {
	ID       string
	Dir      string
	Manifest *Manifest
	State    *RunState
}

// Active reports whether the run is still in progress.
func (r *Run) Active() bool { return !r.State.Phase.Terminal() }

// NewRunID mints the id for a run starting now: run-<UTC timestamp>Z.
func (s *Store) NewRunID() string {
	return "run-" + s.clock().UTC().Format("20060102T150405") + "Z"
}

// CreateRun writes a new run from manifest and points `latest` at it.
//
// It refuses when a run is already in progress: two live runs would each hold
// a snapshot of the same campaign and race each other's verdicts. Close the
// existing one with Abandon first. The store assigns the run id from its
// clock, overwriting whatever the manifest carried.
func (s *Store) CreateRun(ctx context.Context, manifest *Manifest) (*Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if manifest == nil {
		return nil, camperrors.NewValidation("manifest", "is required", nil)
	}
	if err := os.MkdirAll(s.root, dirMode); err != nil {
		return nil, camperrors.Wrapf(err, "create %s", s.root)
	}

	// The whole create is one critical section. Checking for an active run and
	// then creating one are a read-modify-write over the same state: without
	// the lock, two `camp triage start` invocations can both observe no active
	// run and both create one, which is the exact condition the check exists
	// to prevent.
	var run *Run
	err := withLock(ctx, filepath.Join(s.root, createLockName), func() error {
		active, err := s.activeRun(ctx)
		if err != nil {
			return err
		}
		if active != nil {
			return camperrors.Wrap(camperrors.ErrConflict,
				"run "+active.ID+" is still in progress (phase "+string(active.State.Phase)+
					"); close it with `camp triage abandon` before starting another")
		}

		runID := s.NewRunID()
		dir := s.RunDir(runID)
		if _, err := os.Stat(dir); err == nil {
			return camperrors.Wrap(camperrors.ErrAlreadyExists, "triage run "+runID)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return camperrors.Wrapf(err, "stat %s", dir)
		}

		manifest.RunID = runID
		if manifest.CreatedAt.IsZero() {
			manifest.CreatedAt = s.clock()
		}
		state := &RunState{
			RunID: runID,
			Phase: PhaseCreated,
			PhaseHistory: []PhaseTransition{
				{Phase: PhaseCreated, At: s.clock()},
			},
		}

		// Encode both documents before creating anything on disk: an invalid
		// manifest must not leave a half-built run directory behind.
		manifestBytes, err := MarshalDocument(manifest)
		if err != nil {
			return err
		}
		stateBytes, err := MarshalDocument(state)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Join(dir, EvidenceDirName), dirMode); err != nil {
			return camperrors.Wrapf(err, "create run directory %s", dir)
		}
		if err := s.writeLocked(ctx, filepath.Join(dir, ManifestFileName), manifestBytes); err != nil {
			return err
		}
		if err := s.writeLocked(ctx, filepath.Join(dir, RunStateFileName), stateBytes); err != nil {
			return err
		}
		if err := s.setLatest(ctx, runID); err != nil {
			return err
		}

		run = &Run{ID: runID, Dir: dir, Manifest: manifest, State: state}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return run, nil
}

// OpenRun loads the run with the given id.
func (s *Store) OpenRun(ctx context.Context, runID string) (*Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runID == "" {
		return nil, camperrors.NewValidation("run_id", "is required", nil)
	}
	dir := s.RunDir(runID)

	var manifest Manifest
	if err := s.readDocument(filepath.Join(dir, ManifestFileName), &manifest); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, camperrors.NewNotFound("triage run", runID, err)
		}
		return nil, err
	}
	var state RunState
	if err := s.readDocument(filepath.Join(dir, RunStateFileName), &state); err != nil {
		return nil, err
	}

	return &Run{ID: runID, Dir: dir, Manifest: &manifest, State: &state}, nil
}

// OpenLatest loads the run `latest` points at. It returns a NotFoundError when
// the campaign has never run triage.
func (s *Store) OpenLatest(ctx context.Context) (*Run, error) {
	runID, err := s.LatestRunID(ctx)
	if err != nil {
		return nil, err
	}
	return s.OpenRun(ctx, runID)
}

// LatestRunID reads the `latest` pointer.
func (s *Store) LatestRunID(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := filepath.Join(s.root, LatestFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", camperrors.NewNotFound("triage run", "latest", err)
	}
	if err != nil {
		return "", camperrors.Wrapf(err, "read %s", path)
	}
	runID := strings.TrimSpace(string(raw))
	if runID == "" {
		return "", camperrors.NewNotFound("triage run", "latest", nil)
	}
	return runID, nil
}

// SetPhase records a phase transition and returns the updated run.
//
// The transition is written before the caller begins that phase's side
// effects. That ordering is the resume contract: a process killed partway
// through a phase re-opens at the phase whose work may be incomplete and
// redoes it, rather than at the previous phase, which would skip it.
func (s *Store) SetPhase(ctx context.Context, runID string, phase Phase, note string) (*Run, error) {
	run, err := s.mutateState(ctx, runID, func(run *Run) (bool, error) {
		if run.State.Phase == phase {
			return false, nil
		}
		if !run.State.Phase.CanTransitionTo(phase) {
			return false, camperrors.NewValidation("phase",
				"cannot move from "+string(run.State.Phase)+" to "+string(phase), camperrors.ErrInvalidInput)
		}
		run.State.Phase = phase
		run.State.PhaseHistory = append(run.State.PhaseHistory, PhaseTransition{
			Phase: phase,
			At:    s.clock(),
			Note:  note,
		})
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	if err := failpoint.Trigger(ctx, SiteSetPhaseAfterStateWrite); err != nil {
		return nil, err
	}
	return run, nil
}

// Abandon closes a run without deleting anything. State is kept: an abandoned
// run is still the base an incremental run can diff against.
func (s *Store) Abandon(ctx context.Context, runID, reason string) (*Run, error) {
	run, err := s.mutateState(ctx, runID, func(run *Run) (bool, error) {
		if run.State.Phase == PhaseAbandoned {
			return false, nil
		}
		if run.State.Phase.Terminal() {
			return false, camperrors.NewValidation("phase",
				"run "+runID+" already closed as "+string(run.State.Phase), camperrors.ErrInvalidInput)
		}
		run.State.Phase = PhaseAbandoned
		run.State.PhaseHistory = append(run.State.PhaseHistory, PhaseTransition{
			Phase: PhaseAbandoned,
			At:    s.clock(),
			Note:  reason,
		})
		if trimmed := strings.TrimSpace(reason); trimmed != "" {
			run.State.AbandonReason = &trimmed
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	// `latest` keeps pointing at the abandoned run: it is still the most
	// recent one, and the next start reads it to decide what to carry
	// forward. Only its phase says it is closed.
	if err := s.setLatest(ctx, runID); err != nil {
		return nil, err
	}
	return run, nil
}

// --- internals ---------------------------------------------------------

// mutateState applies a change to a run's state under the state file's lock,
// holding it across the read, the change, and the write.
//
// The lock has to span all three. Two commands that each opened the run, each
// appended a phase transition, and each wrote would silently drop one of the
// two transitions, and the phase history is what makes a killed run
// resumable. mutate reports whether anything actually changed, so a no-op
// call does not rewrite the file or record a second identical transition.
func (s *Store) mutateState(ctx context.Context, runID string, mutate func(*Run) (bool, error)) (*Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runID == "" {
		return nil, camperrors.NewValidation("run_id", "is required", nil)
	}

	var run *Run
	err := withLock(ctx, lockPathFor(filepath.Join(s.RunDir(runID), RunStateFileName)), func() error {
		opened, err := s.OpenRun(ctx, runID)
		if err != nil {
			return err
		}
		changed, err := mutate(opened)
		if err != nil {
			return err
		}
		run = opened
		if !changed {
			return nil
		}
		body, err := MarshalDocument(opened.State)
		if err != nil {
			return err
		}
		return writeAtomic(filepath.Join(opened.Dir, RunStateFileName), body)
	})
	if err != nil {
		return nil, err
	}
	return run, nil
}

// activeRun returns the run in progress, or nil when there is none.
func (s *Store) activeRun(ctx context.Context) (*Run, error) {
	run, err := s.OpenLatest(ctx)
	if err != nil {
		var notFound *camperrors.NotFoundError
		if camperrors.As(err, &notFound) {
			return nil, nil
		}
		return nil, err
	}
	if !run.Active() {
		return nil, nil
	}
	return run, nil
}

// setLatest points the pointer file at runID.
func (s *Store) setLatest(ctx context.Context, runID string) error {
	if err := os.MkdirAll(s.root, dirMode); err != nil {
		return camperrors.Wrapf(err, "create %s", s.root)
	}
	return s.writeLocked(ctx, filepath.Join(s.root, LatestFileName), []byte(runID+"\n"))
}
