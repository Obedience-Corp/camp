package triage

import (
	"sort"
	"strconv"
	"strings"
)

// RowClass is one row's standing after a refresh: whether the world it was
// judged against still holds.
//
// Reference: workflow/design/camp-triage/04-data-schemas-and-phases.md,
// "The staleness diff (refresh)".
type RowClass string

const (
	// ClassFresh means identity resolves at the same key and every anchor
	// still observes the value it was judged against. The verdict stands.
	ClassFresh RowClass = "fresh"
	// ClassMoved means the stable id resolved at a new path or stage. The row
	// is re-keyed in place and the verdict stands: identity survives moves,
	// which is the whole reason rows are keyed by stable id (FT-006).
	ClassMoved RowClass = "moved"
	// ClassChanged means an anchor observes a different value than the one
	// the verdict rested on. The verdict goes stale and the row re-queues.
	ClassChanged RowClass = "changed"
	// ClassGone means the stable id is no longer discoverable outside
	// dungeons. The verdict goes stale and the row is flagged for a human:
	// external completion is the likely story (FT-006).
	ClassGone RowClass = "gone"
	// ClassNew means discovery found an item the snapshot does not carry. It
	// is appended to the manifest and queued for judgment.
	ClassNew RowClass = "new"
)

// RowClasses returns the classification vocabulary.
func RowClasses() []string {
	return []string{
		string(ClassFresh),
		string(ClassMoved),
		string(ClassChanged),
		string(ClassGone),
		string(ClassNew),
	}
}

// Applicable reports whether apply may execute a row in this class. Only
// fresh and moved qualify; everything else has lost the basis it was judged
// on. `apply` refuses the rest and lists them (exit 2).
func (c RowClass) Applicable() bool {
	return c == ClassFresh || c == ClassMoved
}

// DiscoveredItem is the slice of a freshly discovered workitem the diff reads.
//
// A narrow struct rather than workitem.WorkItem so the classifier's inputs are
// plain snapshots and a test can state a discovery state in four lines. The
// caller builds these from one Discover() walk.
type DiscoveredItem struct {
	StableID       string
	Key            string
	Type           string
	Title          string
	RelativePath   string
	LifecycleStage string
	AttentionStage string
}

// AnchorCheck is one anchor's re-check result: what the anchor recorded, and
// what refresh observed just now.
type AnchorCheck struct {
	Anchor   Anchor
	Observed string
	// Unchecked reports that refresh could not re-check this anchor at all
	// (offline, unauthenticated, or a kind with no local check). An unchecked
	// anchor is not a mismatch — it is an absence of evidence, counted and
	// reported separately so apply can decide what to do about it. Treating
	// it as a match would let a merged PR apply silently; treating it as a
	// change would make every offline refresh invalidate the whole run.
	Unchecked bool
}

// Matches reports whether the anchor still observes the value it recorded.
// An unchecked anchor never matches and never mismatches; callers must test
// Unchecked first.
func (c AnchorCheck) Matches() bool {
	return c.Observed == c.Anchor.RecordedValue()
}

// DiffInput is everything the classifier reads. All three fields are
// snapshots: the function does no I/O and holds no clock.
type DiffInput struct {
	// Rows is the run's ENTIRE manifest, not the decided subset.
	//
	// The classifier needs every row to tell a genuinely new discovery from a
	// row that is simply still awaiting judgment. Passing only the decided
	// rows would classify every undecided row as new and append duplicates of
	// them to the manifest.
	Rows []ManifestRow
	// Discovered indexes a fresh discovery pass by stable id, already
	// filtered to items outside dungeons. A stable id absent from this map is
	// what "gone" means.
	Discovered map[string]DiscoveredItem
	// Anchors holds the re-check results for each row's evidence anchors,
	// keyed by stable id. A row with no entry simply has no anchors: it can
	// still be fresh, it just carries no expiry.
	Anchors map[string][]AnchorCheck
}

// RowDiff is one row's classification and the reason that produced it.
type RowDiff struct {
	StableID string
	Class    RowClass
	// Reason explains the class in one line, naming the specific anchor or
	// location that decided it. Every row carries one, including fresh rows:
	// "why is this still applicable" is as much a question as the converse.
	Reason string
	// Moved describes the relocation when Class is ClassMoved, and is nil
	// otherwise. The effects layer re-keys the manifest row from it.
	Moved *Relocation
	// Discovered is the freshly discovered item when Class is ClassNew, and
	// nil otherwise. The effects layer builds a manifest row from it, which
	// needs the profile the classifier deliberately does not take.
	Discovered *DiscoveredItem
	// UncheckedAnchors counts anchors refresh could not re-check.
	UncheckedAnchors int
	// ChangedAnchors are the mismatching anchors when Class is ClassChanged.
	// Every mismatch is carried, not just the one that decided the class, so
	// a human re-judging the row sees the full picture.
	ChangedAnchors []AnchorCheck
}

// Relocation is where a moved row went. Both sides are recorded so the
// refresh output can show the move rather than only assert it.
type Relocation struct {
	FromKey            string
	ToKey              string
	FromRelativePath   string
	ToRelativePath     string
	FromLifecycleStage string
	ToLifecycleStage   string
	FromAttentionStage string
	ToAttentionStage   string
}

// Diff is the classification of a whole run.
type Diff struct {
	// Rows are in canonical order: manifest order first (which is already
	// sorted by (type, key) from the snapshot), then new discoveries sorted
	// by key. Two refreshes over an unchanged campaign emit identical output.
	Rows []RowDiff
}

// ClassifyRows classifies every manifest row and every fresh discovery.
//
// Pure function: no I/O, no mutation, no clock, no context to cancel. The
// caller gathers the discovery walk and the anchor checks, this decides what
// they mean, and the effects layer acts. Modelled on workitem.PlanSweep for
// the same reason — a classification worth trusting is one that can be stated
// as a table test.
//
// Precedence is gone > changed > moved > fresh, and it is deliberate. A row
// can satisfy several predicates at once (an item that moved AND whose anchor
// hash changed), and in every such case the safety-dominant class has to win:
// classifying that row `moved` would let a stale verdict apply.
func ClassifyRows(in DiffInput) Diff {
	seen := make(map[string]bool, len(in.Rows))
	out := make([]RowDiff, 0, len(in.Rows))

	for _, row := range in.Rows {
		seen[row.StableID] = true
		out = append(out, classifyRow(row, in.Discovered[row.StableID],
			hasKey(in.Discovered, row.StableID), in.Anchors[row.StableID]))
	}

	return Diff{Rows: append(out, newRows(in.Discovered, seen)...)}
}

// classifyRow decides one manifest row's class.
func classifyRow(row ManifestRow, item DiscoveredItem, found bool, checks []AnchorCheck) RowDiff {
	diff := RowDiff{
		StableID:         row.StableID,
		UncheckedAnchors: countUnchecked(checks),
	}

	if !found {
		diff.Class = ClassGone
		diff.Reason = "no longer discoverable outside dungeons at " +
			quote(row.RelativePath) + "; external completion is the likely story"
		return diff
	}

	// Anchors first: a changed anchor invalidates the verdict wherever the
	// item now lives, so it outranks the location comparison below.
	if changed := mismatched(checks); len(changed) > 0 {
		diff.Class = ClassChanged
		diff.ChangedAnchors = changed
		diff.Reason = anchorChangeReason(changed)
		return diff
	}

	move := relocationBetween(row, item)

	// A path-bound row (FT-008) has no identity beyond where it sits, so a
	// move does not preserve its verdict the way a marker-backed move does.
	// It is `changed` rather than `gone`: the item is still right there, and
	// telling a human it vanished would point them at the wrong story.
	if move != nil && row.IdentityException != nil {
		diff.Class = ClassChanged
		diff.Reason = "identity exception was bound to " +
			quote(row.IdentityException.Path) + " and the row moved to " +
			quote(item.RelativePath) + "; adopt it to give it a durable id"
		return diff
	}

	if move != nil {
		diff.Class = ClassMoved
		diff.Moved = move
		diff.Reason = move.reason()
		return diff
	}

	diff.Class = ClassFresh
	diff.Reason = freshReason(len(checks), diff.UncheckedAnchors)
	return diff
}

// newRows returns a RowDiff for every discovery the manifest does not carry,
// sorted by key so the output is deterministic.
func newRows(discovered map[string]DiscoveredItem, seen map[string]bool) []RowDiff {
	var fresh []DiscoveredItem
	for id, item := range discovered {
		if !seen[id] {
			fresh = append(fresh, item)
		}
	}
	sort.Slice(fresh, func(a, b int) bool { return fresh[a].Key < fresh[b].Key })

	out := make([]RowDiff, 0, len(fresh))
	for i := range fresh {
		item := fresh[i]
		out = append(out, RowDiff{
			StableID:   item.StableID,
			Class:      ClassNew,
			Reason:     "discovered at " + quote(item.RelativePath) + " but absent from the run snapshot",
			Discovered: &item,
		})
	}
	return out
}

// relocationBetween returns where the row moved to, or nil when it is still at
// the same key, path, and stages.
func relocationBetween(row ManifestRow, item DiscoveredItem) *Relocation {
	move := &Relocation{
		FromKey:            row.Key,
		ToKey:              item.Key,
		FromRelativePath:   row.RelativePath,
		ToRelativePath:     item.RelativePath,
		FromLifecycleStage: row.LifecycleStage,
		ToLifecycleStage:   item.LifecycleStage,
		FromAttentionStage: row.AttentionStage,
		ToAttentionStage:   item.AttentionStage,
	}
	if move.FromKey == move.ToKey &&
		move.FromRelativePath == move.ToRelativePath &&
		move.FromLifecycleStage == move.ToLifecycleStage &&
		move.FromAttentionStage == move.ToAttentionStage {
		return nil
	}
	return move
}

// reason describes a relocation, naming only the dimensions that actually
// differ so the output says "stage advanced" rather than repeating an
// unchanged path back at the reader.
func (r Relocation) reason() string {
	var parts []string
	if r.FromRelativePath != r.ToRelativePath {
		parts = append(parts, "path "+quote(r.FromRelativePath)+" -> "+quote(r.ToRelativePath))
	}
	if r.FromLifecycleStage != r.ToLifecycleStage {
		parts = append(parts, "lifecycle "+quote(r.FromLifecycleStage)+" -> "+quote(r.ToLifecycleStage))
	}
	if r.FromAttentionStage != r.ToAttentionStage {
		parts = append(parts, "attention "+quote(r.FromAttentionStage)+" -> "+quote(r.ToAttentionStage))
	}
	if len(parts) == 0 && r.FromKey != r.ToKey {
		parts = append(parts, "key "+quote(r.FromKey)+" -> "+quote(r.ToKey))
	}
	return "identity resolved at a new location: " + strings.Join(parts, ", ") +
		"; verdict stands"
}

// anchorChangeReason names every anchor that moved, with both values.
func anchorChangeReason(changed []AnchorCheck) string {
	parts := make([]string, 0, len(changed))
	for _, check := range changed {
		parts = append(parts, check.Anchor.String()+" "+
			quote(check.Anchor.RecordedValue())+" -> "+quote(check.Observed))
	}
	noun := "anchors"
	if len(changed) == 1 {
		noun = "anchor"
	}
	return noun + " changed: " + strings.Join(parts, ", ")
}

// freshReason explains why a row is still applicable, including the honest
// case where nothing about it was actually verifiable.
func freshReason(total, unchecked int) string {
	switch {
	case total == 0:
		return "identity resolves at the same location; no anchors to check"
	case unchecked == total:
		return "identity resolves at the same location; all " +
			strconv.Itoa(total) + " anchor(s) unchecked"
	case unchecked > 0:
		return "identity resolves at the same location; " +
			strconv.Itoa(total-unchecked) + " of " + strconv.Itoa(total) +
			" anchor(s) match, " + strconv.Itoa(unchecked) + " unchecked"
	default:
		return "identity resolves at the same location; all " +
			strconv.Itoa(total) + " anchor(s) match"
	}
}

// mismatched returns the checked anchors whose observed value differs.
func mismatched(checks []AnchorCheck) []AnchorCheck {
	var out []AnchorCheck
	for _, check := range checks {
		if !check.Unchecked && !check.Matches() {
			out = append(out, check)
		}
	}
	return out
}

// countUnchecked reports how many anchors refresh could not re-check.
func countUnchecked(checks []AnchorCheck) int {
	n := 0
	for _, check := range checks {
		if check.Unchecked {
			n++
		}
	}
	return n
}

// hasKey reports whether the discovery index carries an id, distinguishing a
// missing entry from a zero-valued one.
func hasKey(discovered map[string]DiscoveredItem, id string) bool {
	_, ok := discovered[id]
	return ok
}

// CountByClass tallies the diff by class.
func (d Diff) CountByClass() map[RowClass]int {
	out := make(map[RowClass]int, len(RowClasses()))
	for _, row := range d.Rows {
		out[row.Class]++
	}
	return out
}

// InClass returns the rows in one class, preserving canonical order.
func (d Diff) InClass(class RowClass) []RowDiff {
	var out []RowDiff
	for _, row := range d.Rows {
		if row.Class == class {
			out = append(out, row)
		}
	}
	return out
}

// RowsWithUncheckedAnchors counts rows carrying at least one anchor refresh
// could not verify. Spec doc 04 requires the diff report this, because it is
// the difference between "nothing changed" and "we could not look".
func (d Diff) RowsWithUncheckedAnchors() int {
	n := 0
	for _, row := range d.Rows {
		if row.UncheckedAnchors > 0 {
			n++
		}
	}
	return n
}

// ApplyReadiness is whether apply may execute one row, and when not, which
// kind of refusal it is.
//
// This is the typed result spec doc 04's offline rule needs. Apply lands in
// the next sequence, but the rule belongs with the classification that
// produces it, so the two cannot drift.
type ApplyReadiness string

const (
	// ApplyReady means the row may be executed.
	ApplyReady ApplyReadiness = "ready"
	// ApplyBlockedStale means the row is not fresh or moved. Its verdict
	// rests on something that changed, so it needs re-judging, not a flag.
	ApplyBlockedStale ApplyReadiness = "blocked-stale"
	// ApplyBlockedUnchecked means the row carries an anchor refresh could not
	// verify and the action is terminal. --force overrides this one, because
	// unlike a stale verdict it reports missing information rather than
	// contradicted information, and an operator may know what camp could not
	// observe.
	ApplyBlockedUnchecked ApplyReadiness = "blocked-unchecked"
)

// Blocked reports whether the readiness refuses execution.
func (r ApplyReadiness) Blocked() bool { return r != ApplyReady }

// ApplyReadinessFor decides whether apply may execute a row.
//
// An unchecked anchor is fresh for a non-terminal action and blocking for a
// terminal one. That asymmetry is the whole design: parking a workitem over an
// unverified PR state is recoverable in one command, while retiring one is the
// operation that has to be right the first time.
func ApplyReadinessFor(diff RowDiff, action CanonicalAction, force bool) ApplyReadiness {
	if !diff.Class.Applicable() {
		return ApplyBlockedStale
	}
	if diff.UncheckedAnchors > 0 && action.Terminal() && !force {
		return ApplyBlockedUnchecked
	}
	return ApplyReady
}
