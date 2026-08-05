package triage

import (
	"strings"
	"time"
)

// CommandKind classifies one planned command by the canonical action family
// it carries out. Apply orders by kind: splits first, then parent moves, then
// attention changes, which have no dependencies.
type CommandKind string

const (
	CommandKindSplit     CommandKind = "split"
	CommandKindDungeon   CommandKind = "dungeon"
	CommandKindRail      CommandKind = "rail"
	CommandKindAttention CommandKind = "attention"
)

// CommandKinds returns the command kind vocabulary in apply order.
func CommandKinds() []string {
	return []string{
		string(CommandKindSplit),
		string(CommandKindDungeon),
		string(CommandKindRail),
		string(CommandKindAttention),
	}
}

// PreconditionKind is a check that must hold before an entry executes.
type PreconditionKind string

const (
	// PreconditionSuccessorsExist gates a parent's terminal promotion on the
	// successors a split declared (D2's successors-before-archive invariant).
	PreconditionSuccessorsExist PreconditionKind = "successors-exist"
	// PreconditionRowFresh requires the row to have survived the staleness
	// diff as fresh or moved.
	PreconditionRowFresh PreconditionKind = "row-fresh"
)

// PreconditionKinds returns the precondition vocabulary.
func PreconditionKinds() []string {
	return []string{
		string(PreconditionSuccessorsExist),
		string(PreconditionRowFresh),
	}
}

// ApplyPlan is `apply-plan.json`: approved verdicts compiled into ordered
// commands. Compilation is pure — the same verdicts always compile to the same
// plan — so a plan can be printed, reviewed, and re-derived at will.
type ApplyPlan struct {
	SchemaVersion string           `json:"schema_version"`
	RunID         string           `json:"run_id"`
	CreatedAt     time.Time        `json:"created_at"`
	Entries       []ApplyPlanEntry `json:"entries"`
}

// ApplyPlanEntry is everything apply will do for one row.
type ApplyPlanEntry struct {
	StableID  string    `json:"stable_id"`
	VerdictAt time.Time `json:"verdict_at"`
	// Commands run in order. Each is an argv camp executes through its own
	// existing services, never a shell string.
	Commands      []PlanCommand  `json:"commands"`
	Preconditions []Precondition `json:"preconditions"`
	// Undo is the command list that reverses this entry, recorded before it
	// runs so a receipt can carry it even when apply fails midway.
	Undo []string `json:"undo"`
	// Blocked is why this entry cannot execute, and empty when it can.
	//
	// A blocked entry still compiles and still prints in a dry-run. The
	// operator is deciding about a portfolio, and an entry that silently
	// vanished because camp cannot run it yet would make the plan look
	// smaller than the decision actually is. Consolidation rows are blocked
	// until phase 004 ships `camp workitem split`, which is what lets these
	// phases land in order.
	Blocked string `json:"blocked,omitempty"`
}

// Executable reports whether apply may run this entry.
func (e ApplyPlanEntry) Executable() bool {
	return e.Blocked == "" && len(e.Commands) > 0
}

// PlanCommand is one argv with the action family it belongs to.
type PlanCommand struct {
	Argv []string    `json:"argv"`
	Kind CommandKind `json:"kind"`
}

// String renders the argv as a copy-pasteable command line. The dry-run
// printer and the receipt both go through this, so what a human reads is
// always a rendering of what actually ran.
func (c PlanCommand) String() string { return strings.Join(c.Argv, " ") }

// Precondition is a check apply evaluates before an entry runs.
type Precondition struct {
	Kind PreconditionKind `json:"kind"`
	// IDs are the subjects of the check, e.g. the successor ids that must
	// exist. Empty for checks that need no subject.
	IDs []string `json:"ids,omitempty"`
}

// Normalize implements Document.
func (p *ApplyPlan) Normalize() {
	p.SchemaVersion = SchemaVersion
	normalizeTime(&p.CreatedAt)
	if p.Entries == nil {
		p.Entries = []ApplyPlanEntry{}
	}
	for i := range p.Entries {
		entry := &p.Entries[i]
		normalizeTime(&entry.VerdictAt)
		if entry.Commands == nil {
			entry.Commands = []PlanCommand{}
		}
		if entry.Preconditions == nil {
			entry.Preconditions = []Precondition{}
		}
		entry.Undo = normalizeStrings(entry.Undo)
		for j := range entry.Commands {
			entry.Commands[j].Argv = normalizeStrings(entry.Commands[j].Argv)
		}
	}
}

func (p *ApplyPlan) kind() string    { return "apply plan" }
func (p *ApplyPlan) version() string { return p.SchemaVersion }

// Validate implements Document.
func (p *ApplyPlan) Validate() []Violation {
	var out []Violation
	out = append(out, checkRequired("run_id", p.RunID)...)
	out = append(out, checkTimeSet("created_at", p.CreatedAt)...)
	for i := range p.Entries {
		out = append(out, p.Entries[i].validate(indexPath("entries", i))...)
	}
	return out
}

// validate reports every rule this entry violates.
func (e *ApplyPlanEntry) validate(path string) []Violation {
	var out []Violation
	out = append(out, checkRequired(joinPath(path, "stable_id"), e.StableID)...)
	out = append(out, checkTimeSet(joinPath(path, "verdict_at"), e.VerdictAt)...)
	// A blocked entry is allowed to carry no commands: there is nothing to
	// run yet, and the reason is the content. Anything else with no commands
	// is a compiler bug rather than a legitimate plan.
	if len(e.Commands) == 0 && e.Blocked == "" {
		out = append(out, Violation{
			Field:   joinPath(path, "commands"),
			Message: "is required: an entry with nothing to run does not belong in a plan",
		})
	}
	for i := range e.Commands {
		cpath := indexPath(joinPath(path, "commands"), i)
		out = append(out, checkEnum(
			joinPath(cpath, "kind"), string(e.Commands[i].Kind), CommandKinds())...)
		if len(e.Commands[i].Argv) == 0 {
			out = append(out, Violation{
				Field:   joinPath(cpath, "argv"),
				Message: "is required",
			})
		}
	}
	for i := range e.Preconditions {
		ppath := indexPath(joinPath(path, "preconditions"), i)
		pre := e.Preconditions[i]
		out = append(out, checkEnum(joinPath(ppath, "kind"), string(pre.Kind), PreconditionKinds())...)
		if pre.Kind == PreconditionSuccessorsExist && len(pre.IDs) == 0 {
			out = append(out, Violation{
				Field:   joinPath(ppath, "ids"),
				Message: "is required for kind " + quote(string(PreconditionSuccessorsExist)),
			})
		}
	}
	return out
}

// ReceiptResult is what happened to one executed command.
type ReceiptResult string

const (
	ReceiptApplied ReceiptResult = "applied"
	ReceiptFailed  ReceiptResult = "failed"
	ReceiptSkipped ReceiptResult = "skipped"
)

// ReceiptResults returns the receipt result vocabulary.
func ReceiptResults() []string {
	return []string{
		string(ReceiptApplied),
		string(ReceiptFailed),
		string(ReceiptSkipped),
	}
}

// Receipt is one line of `receipts.jsonl`: what actually ran, as opposed to
// what was planned. Verification reads receipts, and apply reads them to skip
// work already done, which is what makes a re-run idempotent.
type Receipt struct {
	SchemaVersion string        `json:"schema_version"`
	StableID      string        `json:"stable_id"`
	Argv          []string      `json:"argv"`
	Kind          CommandKind   `json:"kind"`
	StartedAt     time.Time     `json:"started_at"`
	FinishedAt    time.Time     `json:"finished_at"`
	Result        ReceiptResult `json:"result"`
	// Error is the failure text, required when Result is failed.
	Error string `json:"error,omitempty"`
	// Undo is the command that reverses this one.
	Undo string `json:"undo,omitempty"`
	// Commit is the hash camp's auto-commit produced for this action, when
	// the action committed.
	Commit string `json:"commit,omitempty"`
}

// Normalize implements Document.
func (r *Receipt) Normalize() {
	r.SchemaVersion = SchemaVersion
	r.Argv = normalizeStrings(r.Argv)
	normalizeTime(&r.StartedAt)
	normalizeTime(&r.FinishedAt)
}

func (r *Receipt) kind() string    { return "receipt" }
func (r *Receipt) version() string { return r.SchemaVersion }

// Validate implements Document.
func (r *Receipt) Validate() []Violation {
	var out []Violation
	out = append(out, checkRequired("stable_id", r.StableID)...)
	out = append(out, checkEnum("kind", string(r.Kind), CommandKinds())...)
	out = append(out, checkEnum("result", string(r.Result), ReceiptResults())...)
	out = append(out, checkTimeSet("started_at", r.StartedAt)...)
	if len(r.Argv) == 0 {
		out = append(out, Violation{Field: "argv", Message: "is required"})
	}
	switch r.Result {
	case ReceiptFailed:
		out = append(out, checkRequired("error", r.Error)...)
	case ReceiptApplied:
		out = append(out, checkTimeSet("finished_at", r.FinishedAt)...)
		if !r.FinishedAt.IsZero() && r.FinishedAt.Before(r.StartedAt) {
			out = append(out, Violation{
				Field:   "finished_at",
				Message: "is before started_at",
			})
		}
	}
	return out
}

// VerificationResult is whether a row landed where its verdict said it would.
type VerificationResult string

const (
	VerificationMatch    VerificationResult = "match"
	VerificationMismatch VerificationResult = "mismatch"
)

// VerificationResults returns the verification result vocabulary.
func VerificationResults() []string {
	return []string{string(VerificationMatch), string(VerificationMismatch)}
}

// VerificationReport is `verification.json`: post-apply proof, produced by
// re-running discovery rather than by trusting the plan.
type VerificationReport struct {
	SchemaVersion string             `json:"schema_version"`
	RunID         string             `json:"run_id"`
	CheckedAt     time.Time          `json:"checked_at"`
	Rows          []VerificationRow  `json:"rows"`
	Totals        VerificationTotals `json:"totals"`
}

// VerificationRow compares one applied row's expected state to its discovered
// state.
type VerificationRow struct {
	StableID        string             `json:"stable_id"`
	ExpectedPath    string             `json:"expected_path"`
	ExpectedStage   string             `json:"expected_stage"`
	DiscoveredPath  string             `json:"discovered_path"`
	DiscoveredStage string             `json:"discovered_stage"`
	Result          VerificationResult `json:"result"`
	// Explanation is the human note that accounts for a mismatch. A
	// mismatch without one is what `camp triage verify` exits non-zero on.
	Explanation string `json:"explanation,omitempty"`
}

// VerificationTotals is the tally the report renders.
type VerificationTotals struct {
	Checked    int `json:"checked"`
	Matched    int `json:"matched"`
	Mismatched int `json:"mismatched"`
}

// Normalize implements Document. It recomputes the totals from the rows so the
// tally can never disagree with the evidence beneath it.
func (v *VerificationReport) Normalize() {
	v.SchemaVersion = SchemaVersion
	normalizeTime(&v.CheckedAt)
	if v.Rows == nil {
		v.Rows = []VerificationRow{}
	}
	totals := VerificationTotals{Checked: len(v.Rows)}
	for _, row := range v.Rows {
		switch row.Result {
		case VerificationMatch:
			totals.Matched++
		case VerificationMismatch:
			totals.Mismatched++
		}
	}
	v.Totals = totals
}

func (v *VerificationReport) kind() string    { return "verification report" }
func (v *VerificationReport) version() string { return v.SchemaVersion }

// Validate implements Document.
func (v *VerificationReport) Validate() []Violation {
	var out []Violation
	out = append(out, checkRequired("run_id", v.RunID)...)
	out = append(out, checkTimeSet("checked_at", v.CheckedAt)...)

	counted := VerificationTotals{Checked: len(v.Rows)}
	for i := range v.Rows {
		path := indexPath("rows", i)
		row := v.Rows[i]
		out = append(out, checkRequired(joinPath(path, "stable_id"), row.StableID)...)
		out = append(out, checkRequired(joinPath(path, "expected_path"), row.ExpectedPath)...)
		out = append(out, checkEnum(
			joinPath(path, "result"), string(row.Result), VerificationResults())...)
		switch row.Result {
		case VerificationMatch:
			counted.Matched++
		case VerificationMismatch:
			counted.Mismatched++
		}
	}
	if v.Totals != counted {
		out = append(out, Violation{
			Field:   "totals",
			Message: "does not match the rows it summarizes",
		})
	}
	return out
}
