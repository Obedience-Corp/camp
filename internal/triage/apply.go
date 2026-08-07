package triage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// MoveOutcome is what one executed command actually did.
type MoveOutcome struct {
	// Undo is the command that reverses what just happened, derived from
	// where the item really landed rather than from where the plan guessed it
	// would. A dungeon promote's destination carries a dated bucket, so this
	// is the only place the true undo can be known.
	Undo string
	// Commit is the auto-commit hash, empty when the action did not commit.
	Commit string
}

// Mover performs planned commands through camp's existing services.
//
// An interface rather than direct calls for two reasons. It keeps
// internal/triage from importing the command layer, which would invert the
// dependency direction; and it lets the executor's ordering, resume, and
// failure behavior be tested against a fake instead of against real directory
// moves. The real implementation delegates to the same services the workitem
// commands call — spec doc 01's no-new-movers rule — and never re-enters camp
// as a subprocess.
type Mover interface {
	Stage(ctx context.Context, stableID, stage string) (MoveOutcome, error)
	Promote(ctx context.Context, stableID, target string) (MoveOutcome, error)
	Split(ctx context.Context, stableID string, successors []string) (MoveOutcome, error)
	// MoveIdea moves a file-backed workitem through the idea lifecycle.
	// Separate from Promote because camp's services are separate: a directory
	// workitem is promoted, an idea is moved, and neither accepts the other's
	// items.
	MoveIdea(ctx context.Context, stableID, status, reason string) (MoveOutcome, error)
}

// ApplyInput is one apply pass.
type ApplyInput struct {
	RunID string
	Plan  *ApplyPlan
	Mover Mover
	Actor string
	Now   Clock
	// Readiness is each row's apply readiness, from the refresh that ran
	// first. A row missing from this map has not been refreshed and is
	// refused rather than assumed fresh.
	Readiness map[string]ApplyReadiness
}

// SkippedRow is a row apply declined to execute, with the reason.
type SkippedRow struct {
	StableID string `json:"stable_id"`
	Reason   string `json:"reason"`
}

// ApplyResult is what an apply pass did.
type ApplyResult struct {
	RunID string
	// Applied lists rows whose every command succeeded.
	Applied []string
	// Failed names the row that stopped the run, empty when none did.
	Failed string
	// Skipped lists rows not executed, each with why: already applied,
	// blocked by staleness, or blocked waiting on a verb that has not landed.
	Skipped []SkippedRow
	// Receipts are the receipts this pass appended.
	Receipts []Receipt
	// Halted reports that a failure stopped the pass with work remaining.
	Halted bool
}

// Apply executes a compiled plan row by row, writing a receipt per command.
//
// Failure stops the pass. Continuing past a failed move would apply later
// entries against a campaign that is not in the state the plan was compiled
// for, and the rows after the failure are exactly the ones most likely to
// depend on it. The remaining rows stay pending, apply reports what stopped
// it, and a re-run picks up from the first row without an applied receipt.
func (s *Store) Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if in.RunID == "" {
		return nil, camperrors.NewValidation("run_id", "is required", nil)
	}
	if in.Plan == nil {
		return nil, camperrors.NewValidation("plan", "is required", nil)
	}
	if in.Mover == nil {
		return nil, camperrors.NewValidation("mover", "is required", nil)
	}
	if in.Actor == "" {
		return nil, camperrors.NewValidation("actor", "is required", nil)
	}
	if in.Now == nil {
		in.Now = s.clock
	}

	done, err := s.AppliedRows(ctx, in.RunID)
	if err != nil {
		return nil, err
	}

	// The phase moves before the first action, so a killed apply re-opens as
	// `applying` and the operator can see the run was mid-flight rather than
	// merely unfinished.
	if _, err := s.SetPhase(ctx, in.RunID, PhaseApplying, "apply started"); err != nil {
		return nil, err
	}

	result := &ApplyResult{RunID: in.RunID}
	for _, entry := range in.Plan.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if skip, reason := s.skipReason(entry, done, in.Readiness); skip {
			result.Skipped = append(result.Skipped, SkippedRow{
				StableID: entry.StableID, Reason: reason,
			})
			continue
		}
		if halted, err := s.applyEntry(ctx, in, entry, result); err != nil {
			return nil, err
		} else if halted {
			result.Halted = true
			result.Failed = entry.StableID
			break
		}
		result.Applied = append(result.Applied, entry.StableID)
	}
	return result, nil
}

// skipReason reports whether an entry is skipped and why.
func (s *Store) skipReason(
	entry ApplyPlanEntry, done map[string]bool, readiness map[string]ApplyReadiness,
) (bool, string) {
	if done[entry.StableID] {
		return true, "already applied in an earlier pass"
	}
	if entry.Blocked != "" {
		return true, entry.Blocked
	}
	ready, checked := readiness[entry.StableID]
	if !checked {
		// Not refused for being stale — refused for never having been
		// checked. Assuming fresh here would defeat the refresh entirely.
		return true, "no refresh result for this row; run camp triage refresh"
	}
	if ready.Blocked() {
		return true, string(ready)
	}
	return false, ""
}

// applyEntry runs one entry's commands, appending a receipt after each.
// It reports whether the pass should halt.
func (s *Store) applyEntry(
	ctx context.Context, in ApplyInput, entry ApplyPlanEntry, result *ApplyResult,
) (bool, error) {
	for _, command := range entry.Commands {
		started := in.Now()
		outcome, execErr := s.runCommand(ctx, in.Mover, entry, command)
		receipt := Receipt{
			StableID:   entry.StableID,
			Argv:       command.Argv,
			Kind:       command.Kind,
			StartedAt:  started,
			FinishedAt: in.Now(),
			Result:     ReceiptApplied,
			Undo:       outcome.Undo,
			Commit:     outcome.Commit,
		}
		if execErr != nil {
			receipt.Result = ReceiptFailed
			receipt.Error = execErr.Error()
			receipt.Undo = ""
			receipt.Commit = ""
		}
		if err := s.AppendReceipt(ctx, in.RunID, receipt); err != nil {
			return false, err
		}
		result.Receipts = append(result.Receipts, receipt)
		if execErr != nil {
			return true, nil
		}
	}
	return false, nil
}

// runCommand dispatches one planned command to the mover.
//
// It routes on Kind rather than re-parsing Argv: the argv is the audit
// representation, and parsing it back would make the record the executor
// depends on, which is exactly backwards.
func (s *Store) runCommand(
	ctx context.Context, mover Mover, entry ApplyPlanEntry, command PlanCommand,
) (MoveOutcome, error) {
	switch command.Kind {
	case CommandKindAttention:
		return mover.Stage(ctx, entry.StableID, argAfterStage(command.Argv))
	case CommandKindRail, CommandKindDungeon:
		return mover.Promote(ctx, entry.StableID, argAfterFlag(command.Argv, "--target"))
	case CommandKindSplit:
		return mover.Split(ctx, entry.StableID, argsAfterFlags(command.Argv, "--into"))
	case CommandKindIdea:
		return mover.MoveIdea(ctx, entry.StableID,
			argAfterStage(command.Argv), argAfterFlag(command.Argv, "--reason"))
	}
	return MoveOutcome{}, camperrors.NewValidation("kind",
		"unknown command kind "+quote(string(command.Kind)), camperrors.ErrInvalidInput)
}

// argAfterStage reads the positional stage from a stage argv:
// camp workitem stage <id> <stage>.
func argAfterStage(argv []string) string {
	if len(argv) < 5 {
		return ""
	}
	return argv[4]
}

// argAfterFlag returns the value following a flag.
func argAfterFlag(argv []string, flag string) string {
	for i, arg := range argv {
		if arg == flag && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}

// argsAfterFlags returns every value following a repeated flag.
func argsAfterFlags(argv []string, flag string) []string {
	var out []string
	for i, arg := range argv {
		if arg == flag && i+1 < len(argv) {
			out = append(out, argv[i+1])
		}
	}
	return out
}

// AppendReceipt records one executed command.
func (s *Store) AppendReceipt(ctx context.Context, runID string, receipt Receipt) error {
	receipt.Normalize()
	// MarshalLine, not MarshalDocument: receipts.jsonl is one record per
	// line, and the indented form would split a single receipt across many.
	body, err := MarshalLine(&receipt)
	if err != nil {
		return err
	}
	return s.appendLine(ctx, filepath.Join(s.RunDir(runID), ReceiptsFileName), body)
}

// Receipts reads a run's receipts in append order.
func (s *Store) Receipts(ctx context.Context, runID string) ([]Receipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.RunDir(runID), ReceiptsFileName)

	// Read under the stream's lock, for the same reason Decisions does: an
	// unlocked read can land between an appender's write and its fsync and
	// see a partial final line.
	var raw []byte
	err := withLock(ctx, lockPathFor(path), func() error {
		var readErr error
		raw, readErr = os.ReadFile(path)
		return readErr
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "read %s", path)
	}

	var receipts []Receipt
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var receipt Receipt
		if err := ParseDocument([]byte(line), &receipt, Lenient); err != nil {
			return nil, camperrors.Wrapf(err, "%s line %d", path, i+1)
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

// AppliedRows reports which rows are already fully applied, so a re-run skips
// them rather than moving a directory twice.
//
// A row counts as applied when it has at least one applied receipt and no
// failed one after it. A row whose last receipt failed is deliberately NOT
// applied: it is where the previous pass stopped, and it is the row the next
// pass should retry first.
func (s *Store) AppliedRows(ctx context.Context, runID string) (map[string]bool, error) {
	receipts, err := s.Receipts(ctx, runID)
	if err != nil {
		return nil, err
	}
	applied := make(map[string]bool, len(receipts))
	for _, receipt := range receipts {
		switch receipt.Result {
		case ReceiptApplied:
			applied[receipt.StableID] = true
		case ReceiptFailed:
			applied[receipt.StableID] = false
		}
	}
	return applied, nil
}

// ApplyReadinessFromDiff builds the readiness map apply consumes from a
// refresh, given each row's approved action.
func ApplyReadinessFromDiff(diff Diff, actions map[string]CanonicalAction, force bool) map[string]ApplyReadiness {
	out := make(map[string]ApplyReadiness, len(diff.Rows))
	for _, row := range diff.Rows {
		out[row.StableID] = ApplyReadinessFor(row, actions[row.StableID], force)
	}
	return out
}
