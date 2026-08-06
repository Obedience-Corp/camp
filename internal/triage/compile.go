package triage

import (
	"sort"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

// CompileInput is everything the plan compiler reads. All of it is snapshot
// data: compiling does no I/O and reads no clock beyond the injected Now.
type CompileInput struct {
	RunID string
	Rows  []ManifestRow
	// Verdicts is the folded decision state. Only approved verdicts compile;
	// anything else is either undecided or already retired.
	Verdicts map[string]RowVerdict
	// Successors names the declared successors of each consolidation row,
	// keyed by the parent's stable id.
	Successors map[string][]string
	// SplitAvailable reports whether `camp workitem split` exists yet. Phase
	// 004 ships the verb; until then consolidation rows compile as blocked
	// rather than failing the plan, so the phases can land in order.
	SplitAvailable bool
	Now            time.Time
}

// CompilePlan turns approved verdicts into an ordered, executable plan.
//
// Pure function on the PlanSweep precedent: the same verdicts always compile
// to the same plan. That is what makes `--dry-run` worth trusting — what it
// prints is what runs, not a second derivation that could differ from the
// first.
func CompilePlan(in CompileInput) (*ApplyPlan, error) {
	entries := make([]ApplyPlanEntry, 0, len(in.Rows))
	for _, row := range in.Rows {
		verdict := in.Verdicts[row.StableID]
		if !verdict.Applicable() {
			continue
		}
		entries = append(entries, compileEntry(row, verdict, in))
	}
	sortPlanEntries(entries)

	plan := &ApplyPlan{RunID: in.RunID, CreatedAt: in.Now, Entries: entries}
	plan.Normalize()
	if err := newValidationError("apply plan", plan.Validate()); err != nil {
		return nil, err
	}
	return plan, nil
}

// compileEntry builds one row's entry.
func compileEntry(row ManifestRow, verdict RowVerdict, in CompileInput) ApplyPlanEntry {
	entry := ApplyPlanEntry{
		StableID:  row.StableID,
		VerdictAt: verdict.At,
		// Every entry requires its row to still be fresh. Recording it as a
		// precondition keeps the plan self-describing instead of relying on
		// the executor to remember a rule that lives somewhere else.
		Preconditions: []Precondition{{Kind: PreconditionRowFresh}},
	}

	if verdict.CanonicalAction == ActionSplit {
		return compileSplitEntry(entry, row, in)
	}

	entry.Commands = []PlanCommand{{
		Argv: ApplyArgvFor(row, verdict.CanonicalAction),
		Kind: commandKindFor(row, verdict.CanonicalAction),
	}}
	entry.Undo = undoArgvFor(row, verdict.CanonicalAction)
	return entry
}

// compileSplitEntry builds a consolidation: split the parent into its declared
// successors, then retire it once they exist.
func compileSplitEntry(entry ApplyPlanEntry, row ManifestRow, in CompileInput) ApplyPlanEntry {
	successors := append([]string(nil), in.Successors[row.StableID]...)
	sort.Strings(successors)

	// A consolidation with no declared successors has nothing to split into:
	// compiling it as executable would put the display placeholder on a real
	// argv, and apply would extract "<successors>" as a successor name. The
	// verdict is approved but incomplete, so the entry blocks with the reason
	// instead of running.
	if len(successors) == 0 {
		entry.Blocked = blockedNoSuccessors
		return entry
	}

	// D2: the parent is not retired until every successor it declared exists.
	// The precondition carries the ids so the executor checks the specific
	// successors this verdict promised, not merely that a split happened.
	entry.Preconditions = append(entry.Preconditions, Precondition{
		Kind: PreconditionSuccessorsExist,
		IDs:  successors,
	})

	if !in.SplitAvailable {
		entry.Blocked = blockedNeedsSplit + " (successors: " + strings.Join(successors, ", ") + ")"
		return entry
	}

	entry.Commands = []PlanCommand{
		{Argv: splitArgv(row, successors), Kind: CommandKindSplit},
		{Argv: promoteArgv(row, consolidatedParentStatus), Kind: CommandKindDungeon},
	}
	return entry
}

// blockedNeedsSplit is the reason a consolidation cannot run yet.
const blockedNeedsSplit = "requires camp workitem split"

// blockedNoSuccessors is the reason an approved consolidation with no
// declared successors compiles blocked rather than executable.
const blockedNoSuccessors = "consolidation declares no successors; record the successor workitems on the verdict before apply"

// consolidatedParentStatus is where a consolidated parent goes once its
// successors exist. Consolidation is a completion, not an abandonment.
const consolidatedParentStatus = "completed"

// ApplyArgvFor is the command that performs one canonical action, as argv.
//
// The single source of truth for how a disposition becomes a camp invocation.
// Every form here was verified by running the real CLI rather than inferred
// from the spec, which is how two errors in the earlier string-only version
// were caught: the attention verb is `camp workitem stage` and not
// `camp workitem attention`, and it takes the stage positionally rather than
// as `--set`. Spec doc 04's `camp flow move` does not exist at all.
func ApplyArgvFor(row ManifestRow, action CanonicalAction) []string {
	// File-backed workitems move through the idea lifecycle. `camp workitem
	// stage` refuses them outright and `camp workitem promote` cannot resolve
	// them by id, so a plan built from the directory-backed verbs is a plan
	// camp refuses row by row — which is exactly what shipped.
	if isFileBacked(row) {
		return ideaArgvFor(row, action)
	}

	switch action.Family() {
	case ActionFamilyAttention:
		return stageArgv(row.StableID, action.Target())
	case ActionFamilyRail, ActionFamilyDungeon:
		return promoteArgv(row, action.Target())
	}
	if action == ActionSplit {
		return splitArgv(row, nil)
	}
	return nil
}

// isFileBacked reports whether a row is stored as a single file rather than a
// directory. The snapshot records it because the compiler does no I/O and so
// cannot ask the filesystem.
func isFileBacked(row ManifestRow) bool {
	return row.ItemKind == string(workitem.ItemKindFile)
}

// ideaStatusFor maps a canonical action onto `camp idea move`'s vocabulary, or
// "" when the idea lifecycle has no equivalent.
//
// The two vocabularies are close but not identical: a dungeon *completed* is an
// idea that is *done*. Attention stages have no equivalent at all — an idea is
// somewhere in inbox -> ready -> active -> dungeon, and "which lane is it in"
// is not a question its lifecycle answers.
func ideaStatusFor(action CanonicalAction) string {
	switch action {
	case "rail/ready":
		return "ready"
	case "rail/active":
		return "active"
	case "dungeon/completed":
		return "dungeon/done"
	case "dungeon/archived":
		return "dungeon/archived"
	case "dungeon/someday":
		return "dungeon/someday"
	}
	return ""
}

// ideaArgvFor renders the command that moves a file-backed workitem.
//
// Nil for an action the idea lifecycle cannot express, which is how the
// compiler reports "camp has no command for this" rather than emitting one
// that would be refused at apply time.
func ideaArgvFor(row ManifestRow, action CanonicalAction) []string {
	status := ideaStatusFor(action)
	if status == "" {
		return nil
	}
	argv := []string{"camp", "idea", "move", row.StableID, shortIdeaStatus(status)}
	if action.Family() == ActionFamilyDungeon {
		argv = append(argv, "--reason", ideaDungeonReason)
	}
	return argv
}

// shortIdeaStatus renders the CLI's short form of a dungeon status, which is
// what `camp idea move` accepts and prints.
func shortIdeaStatus(status string) string {
	return strings.TrimPrefix(status, "dungeon/")
}

// ideaDungeonReason is what camp records on an idea a triage run retired. Camp
// requires a reason for any dungeon move and appends it to the idea as a
// decision record, so it names the process that decided rather than restating
// the disposition the record already carries.
const ideaDungeonReason = "retired by an approved camp triage verdict"

// ApplyCommandFor renders the command that performs one canonical action.
//
// A thin rendering of ApplyArgvFor, so the string a human is shown and the
// argv the executor records cannot disagree. They did disagree while this was
// a separate mapping: the review surface printed a command camp does not have.
func ApplyCommandFor(row ManifestRow, action CanonicalAction) string {
	return strings.Join(ApplyArgvFor(row, action), " ")
}

// stageArgv sets a workitem's attention stage:
// `camp workitem stage <selector> <current|next|active|parked|clear>`.
func stageArgv(stableID, stage string) []string {
	if stage == "" {
		stage = stageClearArg
	}
	return []string{"camp", "workitem", "stage", stableID, stage}
}

// stageClearArg is what `camp workitem stage` accepts to remove a stage.
const stageClearArg = "clear"

// promoteArgv promotes a workitem to a rail or dungeon target.
func promoteArgv(row ManifestRow, target string) []string {
	return []string{"camp", "workitem", "promote", row.StableID, "--target", target}
}

// splitArgv splits a parent into its declared successors.
func splitArgv(row ManifestRow, successors []string) []string {
	argv := []string{"camp", "workitem", "split", row.StableID}
	if len(successors) == 0 {
		return append(argv, "--into", splitSuccessorPlaceholder)
	}
	for _, successor := range successors {
		argv = append(argv, "--into", successor)
	}
	return argv
}

// splitSuccessorPlaceholder stands in when a consolidation has not declared
// successors, so the rendered command reads as incomplete rather than runnable.
const splitSuccessorPlaceholder = "<successors>"

// undoArgvFor returns the command that reverses an action, or nil when the
// compiler cannot honestly know it.
//
// Attention and rail moves have exact, path-independent inverses. A dungeon
// promote does not: it lands the workitem in a dated bucket
// (`workflow/design/.dungeon/completed/2026-08-05/<name>`), and that date is
// the apply date, which a compiler given a frozen snapshot cannot know. An
// undo built on a guessed date would point at nothing while looking correct,
// so the executor derives the real one from the promote result and records it
// on the receipt. Spec doc 04 already makes receipts, not the plan, what
// verification reads.
func undoArgvFor(row ManifestRow, action CanonicalAction) []string {
	switch action.Family() {
	case ActionFamilyAttention:
		// Back to the stage the snapshot froze, or cleared when it had none.
		return stageArgv(row.StableID, row.AttentionStage)
	case ActionFamilyRail:
		return []string{"camp", "workitem", "demote", row.StableID}
	}
	return nil
}

// commandKindFor tags an action's command with the family it belongs to.
func commandKindFor(row ManifestRow, action CanonicalAction) CommandKind {
	if isFileBacked(row) && ideaStatusFor(action) != "" {
		return CommandKindIdea
	}
	switch action.Family() {
	case ActionFamilyAttention:
		return CommandKindAttention
	case ActionFamilyRail:
		return CommandKindRail
	case ActionFamilyDungeon:
		return CommandKindDungeon
	}
	return CommandKindSplit
}

// planOrder is the execution order of the command kinds.
//
// Splits first: a consolidation creates the successors a parent move depends
// on. Dungeon and rail moves next, because they relocate directories other
// entries may reference. Attention changes last — they are cheap, they depend
// on nothing, and running them last means a failure part-way through the
// expensive work has not already churned metadata that would then need undoing.
var planOrder = map[CommandKind]int{
	CommandKindSplit:     0,
	CommandKindDungeon:   1,
	CommandKindRail:      1,
	CommandKindIdea:      1,
	CommandKindAttention: 2,
}

// sortPlanEntries orders entries by group, then by stable id within a group,
// so two compiles of the same verdicts produce byte-identical plans.
func sortPlanEntries(entries []ApplyPlanEntry) {
	sort.SliceStable(entries, func(a, b int) bool {
		ga, gb := planGroupOf(entries[a]), planGroupOf(entries[b])
		if ga != gb {
			return ga < gb
		}
		return entries[a].StableID < entries[b].StableID
	})
}

// planGroupOf is an entry's ordering group. It reads the kind off the entry's
// first command, falling back to the split group for a blocked consolidation
// so a row that cannot run yet still sorts where it will run.
func planGroupOf(entry ApplyPlanEntry) int {
	if len(entry.Commands) == 0 {
		return planOrder[CommandKindSplit]
	}
	return planOrder[entry.Commands[0].Kind]
}

// ExecutableEntries returns the entries apply may run, in plan order.
func (p *ApplyPlan) ExecutableEntries() []ApplyPlanEntry {
	var out []ApplyPlanEntry
	for _, entry := range p.Entries {
		if entry.Executable() {
			out = append(out, entry)
		}
	}
	return out
}

// BlockedEntries returns the entries that compiled but cannot run.
func (p *ApplyPlan) BlockedEntries() []ApplyPlanEntry {
	var out []ApplyPlanEntry
	for _, entry := range p.Entries {
		if entry.Blocked != "" {
			out = append(out, entry)
		}
	}
	return out
}
