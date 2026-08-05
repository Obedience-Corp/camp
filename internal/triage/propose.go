package triage

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Rationale is the small record that justifies a proposal.
//
// It is deliberately smaller than an evidence record: evidence is what was
// found, a rationale is why that adds up to this disposition. Keeping them
// apart means a revised proposal does not require re-submitting the reading.
type Rationale struct {
	SchemaVersion string `json:"schema_version"`
	Summary       string `json:"summary"`
	// AnchorsUsed names which of the row's evidence anchors the conclusion
	// actually rests on, so a refresh can tell whether a change to one of them
	// invalidates this proposal or merely touched something incidental.
	AnchorsUsed []string   `json:"anchors_used"`
	Confidence  Confidence `json:"confidence"`
	// Successors names the workitems a `consolidate` verdict proposes to
	// split the row into. It is the successor list the apply plan compiles
	// into `camp workitem split --into/--adopt`, and the list the retirement
	// gate then holds the parent against.
	//
	// It lives on the rationale rather than the decision event because it is
	// part of the argument for the verdict, not a second verdict: "retire
	// this once these exist" is one claim. Empty for every other
	// disposition; omitempty, so a non-consolidate rationale serializes
	// exactly as before.
	Successors []string `json:"successors,omitempty"`
}

// Normalize implements Document.
func (r *Rationale) Normalize() {
	r.SchemaVersion = SchemaVersion
	r.AnchorsUsed = normalizeStrings(r.AnchorsUsed)
}

func (r *Rationale) kind() string    { return "rationale" }
func (r *Rationale) version() string { return r.SchemaVersion }

// Validate implements Document.
func (r *Rationale) Validate() []Violation {
	var out []Violation
	out = append(out, checkRequired("summary", r.Summary)...)
	out = append(out, checkEnum("confidence", string(r.Confidence), Confidences())...)
	return out
}

// RationaleDirName holds the rationale files of a run.
const RationaleDirName = "rationales"

// ProposeInput is one proposed disposition.
type ProposeInput struct {
	RunID       string
	StableID    string
	Disposition string
	Rationale   *Rationale
	Actor       string
	Now         time.Time
}

// ProposeResult reports what the proposal did.
type ProposeResult struct {
	StableID        string          `json:"stable_id"`
	Disposition     string          `json:"disposition"`
	CanonicalAction CanonicalAction `json:"canonical_action"`
	RationaleRef    string          `json:"rationale_ref"`
	// Superseded names the disposition this proposal replaced, when it
	// replaced one.
	Superseded string `json:"superseded,omitempty"`
	// RequiresApproval is true when the action is terminal. Recorded so a
	// driver cannot present a split or a dungeon move as already settled.
	RequiresApproval bool `json:"requires_approval"`
}

// Propose records a proposed disposition for a row.
//
// One proposal is live per row, but nothing is overwritten: replacing a
// proposal appends a `superseded` event for the old one and then a `proposed`
// event for the new. The stream keeps the whole argument, which is what lets a
// reviewer see that a row was reconsidered rather than only where it landed.
func (s *Store) Propose(ctx context.Context, in ProposeInput) (*ProposeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if in.Rationale == nil {
		return nil, camperrors.NewValidation("rationale", "is required", camperrors.ErrInvalidInput)
	}
	if strings.TrimSpace(in.Actor) == "" {
		return nil, camperrors.NewValidation("actor", "is required", camperrors.ErrInvalidInput)
	}

	run, err := s.OpenRun(ctx, in.RunID)
	if err != nil {
		return nil, err
	}
	// Recording judgment is what puts a run in the judging phase. The machine
	// is driven by the work, not by an operator naming phases.
	if err := s.AdvancePhase(ctx, in.RunID, PhaseJudging, "first proposal recorded"); err != nil {
		return nil, err
	}
	row, err := run.Row(in.StableID)
	if err != nil {
		return nil, err
	}

	action, err := ResolveDisposition(TypePolicyFor(row.Type), in.Disposition)
	if err != nil {
		return nil, err
	}

	rationaleRef, err := s.writeRationale(ctx, in)
	if err != nil {
		return nil, err
	}

	result := &ProposeResult{
		StableID:         row.StableID,
		Disposition:      in.Disposition,
		CanonicalAction:  action,
		RationaleRef:     rationaleRef,
		RequiresApproval: action.Terminal(),
	}

	// Reading the live proposal and replacing it are one critical section.
	// Two drivers proposing on the same row would otherwise each observe the
	// same proposal and each retire it, leaving two live proposals and one
	// supersede — a stream that misreports how the row was reasoned about,
	// which is the only thing it exists to record.
	err = withLock(ctx, lockPathFor(s.proposeLockPath(in.RunID, row.StableID)), func() error {
		verdicts, err := s.Verdicts(ctx, in.RunID)
		if err != nil {
			return err
		}

		// Retire the live proposal first, so the stream reads in the order
		// the thinking happened rather than showing two at once.
		if previous, ok := verdicts[row.StableID]; ok && HasLiveProposal(previous) {
			if err := s.AppendDecision(ctx, in.RunID, DecisionEvent{
				Event:           DecisionSuperseded,
				StableID:        row.StableID,
				Disposition:     previous.Disposition,
				CanonicalAction: previous.CanonicalAction,
				Actor:           in.Actor,
				At:              in.Now,
				Note:            "replaced by a newer proposal",
			}); err != nil {
				return err
			}
			result.Superseded = previous.Disposition
		}

		return s.AppendDecision(ctx, in.RunID, DecisionEvent{
			Event:           DecisionProposed,
			StableID:        row.StableID,
			Disposition:     in.Disposition,
			CanonicalAction: action,
			RationaleRef:    rationaleRef,
			Actor:           in.Actor,
			At:              in.Now,
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// proposeLockPath is the per-row lock guarding a propose. Per row rather than
// per run, so the fan-out doc 08 expects (several drivers working different
// rows at once) does not serialize on a single lock.
func (s *Store) proposeLockPath(runID, stableID string) string {
	return filepath.Join(s.RunDir(runID), RationaleDirName, recordFileName(stableID))
}

// writeRationale stores the rationale and returns its run-relative path.
func (s *Store) writeRationale(ctx context.Context, in ProposeInput) (string, error) {
	body, err := MarshalDocument(in.Rationale)
	if err != nil {
		return "", err
	}
	name := recordFileName(in.StableID)
	abs := filepath.Join(s.RunDir(in.RunID), RationaleDirName, name)
	if err := os.MkdirAll(filepath.Dir(abs), dirMode); err != nil {
		return "", camperrors.Wrapf(err, "create rationale directory for run %s", in.RunID)
	}
	if err := s.writeLocked(ctx, abs, body); err != nil {
		return "", err
	}
	return path.Join(RationaleDirName, name), nil
}

// Row returns the manifest row with the given stable id.
func (r *Run) Row(stableID string) (*ManifestRow, error) {
	for i := range r.Manifest.Rows {
		if r.Manifest.Rows[i].StableID == stableID {
			return &r.Manifest.Rows[i], nil
		}
	}
	return nil, camperrors.NewNotFound("triage row", stableID+" in run "+r.ID, nil)
}

// ReviewGap is one reason a run is not ready to leave judging.
type ReviewGap struct {
	StableID string `json:"stable_id"`
	Missing  string `json:"missing"`
}

// ReadyForReview reports the rows still missing judgment.
//
// The judging -> reviewing transition is the moment a run stops gathering and
// starts deciding. Letting it through with gaps would put rows in front of a
// reviewer that nobody looked at, and they would be approved by omission.
func (s *Store) ReadyForReview(ctx context.Context, runID string) ([]ReviewGap, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	run, err := s.OpenRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	verdicts, err := s.Verdicts(ctx, runID)
	if err != nil {
		return nil, err
	}

	var gaps []ReviewGap
	for _, row := range run.Manifest.Rows {
		// A carried row was decided in a previous run on purpose.
		if row.CarriedFrom != nil {
			continue
		}
		hasEvidence, err := s.HasEvidence(ctx, runID, row.StableID)
		if err != nil {
			return nil, err
		}
		switch {
		case !hasEvidence:
			gaps = append(gaps, ReviewGap{StableID: row.StableID, Missing: "evidence"})
		case !HasLiveProposal(verdicts[row.StableID]):
			gaps = append(gaps, ReviewGap{StableID: row.StableID, Missing: "proposal"})
		}
	}
	sort.Slice(gaps, func(a, b int) bool { return gaps[a].StableID < gaps[b].StableID })
	return gaps, nil
}

// reviewGapError refuses the transition and names every gap, so the operator
// can close them in one pass.
func reviewGapError(gaps []ReviewGap) error {
	var b strings.Builder
	b.WriteString("cannot start reviewing: ")
	b.WriteString(itoaGaps(len(gaps)))
	for _, gap := range gaps {
		b.WriteString("\n  ")
		b.WriteString(gap.StableID)
		b.WriteString(" needs ")
		b.WriteString(gap.Missing)
	}
	b.WriteString("\n\nRecord one with `camp triage evidence set` / `camp triage propose`,")
	b.WriteString("\nor mark a row judged without a record with `evidence set --no-evidence`.")
	return camperrors.NewValidation("phase", b.String(), camperrors.ErrInvalidInput)
}

func itoaGaps(n int) string {
	if n == 1 {
		return "1 row still needs judgment:"
	}
	return strconv.Itoa(n) + " rows still need judgment:"
}
