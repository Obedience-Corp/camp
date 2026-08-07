package triage

import (
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/dungeon/scaffold"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// DecisionEventKind is one entry type in the append-only verdict stream. A
// row's current verdict is the fold of its events, never a mutable field.
type DecisionEventKind string

const (
	// DecisionProposed records a suggested disposition. One proposal is live
	// per row at a time.
	DecisionProposed DecisionEventKind = "proposed"
	// DecisionApproved records a human accepting the live proposal.
	DecisionApproved DecisionEventKind = "approved"
	// DecisionAmended records a human accepting a different disposition than
	// the one proposed.
	DecisionAmended DecisionEventKind = "amended"
	// DecisionRejected records a human refusing the proposal.
	DecisionRejected DecisionEventKind = "rejected"
	// DecisionSuperseded retires a proposal a newer one replaced.
	DecisionSuperseded DecisionEventKind = "superseded"
	// DecisionStale is written by refresh when an anchor the verdict rested
	// on no longer holds.
	DecisionStale DecisionEventKind = "stale"
)

// DecisionEventKinds returns the event vocabulary.
func DecisionEventKinds() []string {
	return []string{
		string(DecisionProposed),
		string(DecisionApproved),
		string(DecisionAmended),
		string(DecisionRejected),
		string(DecisionSuperseded),
		string(DecisionStale),
	}
}

// DefaultDispositions is the disposition vocabulary of the default profile.
//
// Dispositions are labels, not behavior: a type policy may rename or restrict
// them (doc 05), so this list is what `camp triage propose` offers by default,
// not a schema constraint. What a disposition actually does is its
// CanonicalAction, which is camp's and is validated.
func DefaultDispositions() []string {
	return []string{
		"current", "next", "active", "parked", "ready", "festival",
		"completed", "archived", "someday", "consolidate",
	}
}

// CanonicalAction is the real mutation a disposition resolves to. Labels vary
// per campaign; this does not. It is either a bare verb or "<family>/<target>".
type CanonicalAction string

const (
	// ActionNone records a decision that changes nothing (e.g. "keep as is").
	ActionNone CanonicalAction = "none"
	// ActionSplit runs `camp workitem split`, followed by a gated terminal
	// promote of the parent (D2).
	ActionSplit CanonicalAction = "split"
)

// Canonical action families.
const (
	// ActionFamilyAttention sets a non-terminal attention stage.
	ActionFamilyAttention = "attention"
	// ActionFamilyRail promotes along the workitem rail.
	ActionFamilyRail = "rail"
	// ActionFamilyDungeon promotes into a terminal dungeon status.
	ActionFamilyDungeon = "dungeon"
)

// railTargets are the forward rail promotions `camp workitem promote --target`
// accepts, excluding the dungeon statuses it also takes. Sourced here as
// literals because the command's vocabulary is unexported; the
// TestCanonicalActionsMatchCampVocabulary guard catches drift.
var railTargets = []string{"ready", "active", "festival"}

// CanonicalActions returns every action camp can execute, in family order.
func CanonicalActions() []string {
	out := []string{string(ActionNone), string(ActionSplit)}
	for _, stage := range workitem.AttentionStages() {
		out = append(out, ActionFamilyAttention+"/"+stage)
	}
	for _, target := range railTargets {
		out = append(out, ActionFamilyRail+"/"+target)
	}
	for _, status := range scaffold.StandardStatuses {
		out = append(out, ActionFamilyDungeon+"/"+status)
	}
	return out
}

// Family returns the action's family, or the bare verb for none and split.
func (a CanonicalAction) Family() string {
	if i := strings.IndexByte(string(a), '/'); i >= 0 {
		return string(a)[:i]
	}
	return string(a)
}

// Target returns the part after the family, or "" for a bare verb.
func (a CanonicalAction) Target() string {
	if i := strings.IndexByte(string(a), '/'); i >= 0 {
		return string(a)[i+1:]
	}
	return ""
}

// Terminal reports whether the action retires a workitem. Terminal actions
// always require recorded human approval (D5) and are never applied from a
// proposal alone.
func (a CanonicalAction) Terminal() bool {
	return a.Family() == ActionFamilyDungeon || a == ActionSplit
}

// DecisionEvent is one line of `decisions.jsonl`.
type DecisionEvent struct {
	SchemaVersion string            `json:"schema_version"`
	Event         DecisionEventKind `json:"event"`
	StableID      string            `json:"stable_id"`
	// Disposition is the label recorded. Empty is legal only on a stale or
	// superseded event, which retire a verdict rather than express one.
	Disposition     string          `json:"disposition"`
	CanonicalAction CanonicalAction `json:"canonical_action"`
	// RationaleRef points at the rationale that justified the event, as a
	// run-relative path.
	RationaleRef string `json:"rationale_ref,omitempty"`
	// Actor is the git identity that recorded the event.
	Actor string    `json:"actor"`
	At    time.Time `json:"at"`
	// Note is the free-text `--note` an operator may attach when approving.
	Note string `json:"note,omitempty"`
}

// Normalize implements Document.
func (e *DecisionEvent) Normalize() {
	e.SchemaVersion = SchemaVersion
	normalizeTime(&e.At)
}

func (e *DecisionEvent) kind() string    { return "decision event" }
func (e *DecisionEvent) version() string { return e.SchemaVersion }

// Validate implements Document.
func (e *DecisionEvent) Validate() []Violation {
	var out []Violation
	out = append(out, checkEnum("event", string(e.Event), DecisionEventKinds())...)
	out = append(out, checkRequired("stable_id", e.StableID)...)
	out = append(out, checkRequired("actor", e.Actor)...)
	out = append(out, checkTimeSet("at", e.At)...)

	// stale and superseded retire a verdict; they carry no new one. Every
	// other event asserts a disposition and the action it resolves to.
	switch e.Event {
	case DecisionStale, DecisionSuperseded:
		out = append(out, checkOptionalEnum(
			"canonical_action", string(e.CanonicalAction), CanonicalActions())...)
	default:
		out = append(out, checkRequired("disposition", e.Disposition)...)
		out = append(out, checkEnum(
			"canonical_action", string(e.CanonicalAction), CanonicalActions())...)
	}
	return out
}

// VerdictState is a row's standing after folding its decision events.
type VerdictState string

const (
	// VerdictNone means the row has no events yet.
	VerdictNone VerdictState = ""
	// VerdictProposed means a live proposal awaits approval.
	VerdictProposed VerdictState = "proposed"
	// VerdictApproved means the verdict is ready for apply.
	VerdictApproved VerdictState = "approved"
	// VerdictRejected means the proposal was refused; the row re-queues.
	VerdictRejected VerdictState = "rejected"
	// VerdictStale means refresh invalidated the verdict.
	VerdictStale VerdictState = "stale"
	// VerdictSuperseded means the proposal was replaced and no live one
	// has been recorded since.
	VerdictSuperseded VerdictState = "superseded"
)

// RowVerdict is the folded current standing of one row.
type RowVerdict struct {
	StableID        string
	State           VerdictState
	Disposition     string
	CanonicalAction CanonicalAction
	RationaleRef    string
	Actor           string
	At              time.Time
	Note            string
	// Events is how many events produced this verdict, so callers can tell a
	// first proposal from a long amend history without re-reading the stream.
	Events int
}

// Applicable reports whether apply may execute this verdict.
func (v RowVerdict) Applicable() bool {
	return v.State == VerdictApproved && v.CanonicalAction != ActionNone
}

// FoldDecisions reduces a decision stream to the current verdict of every row
// it mentions, keyed by stable id. Events must be in append order.
//
// Last event wins, with one refinement: stale and superseded retire a verdict
// rather than expressing a new one, so they change the row's state and its
// provenance while leaving the disposition they invalidated visible. That is
// what lets `status` say which verdict went stale instead of only that one
// did, and it is why the stream is folded rather than overwritten in place.
func FoldDecisions(events []DecisionEvent) map[string]RowVerdict {
	verdicts := make(map[string]RowVerdict, len(events))
	for _, event := range events {
		if event.StableID == "" {
			continue
		}
		verdict := verdicts[event.StableID]
		verdict.StableID = event.StableID
		verdict.Events++
		verdict.State = verdictStateFor(event.Event)
		verdict.Actor = event.Actor
		verdict.At = event.At

		// Retiring events carry no verdict of their own; anything they do
		// supply still wins, so a refresh may annotate what it invalidated.
		if event.Disposition != "" {
			verdict.Disposition = event.Disposition
		}
		if event.CanonicalAction != "" {
			verdict.CanonicalAction = event.CanonicalAction
		}
		if event.RationaleRef != "" {
			verdict.RationaleRef = event.RationaleRef
		}
		if event.Note != "" {
			verdict.Note = event.Note
		}
		verdicts[event.StableID] = verdict
	}
	return verdicts
}

// verdictStateFor maps an event kind to the state it leaves the row in. An
// amendment is an approval of a different disposition, so it lands approved.
func verdictStateFor(kind DecisionEventKind) VerdictState {
	switch kind {
	case DecisionProposed:
		return VerdictProposed
	case DecisionApproved, DecisionAmended:
		return VerdictApproved
	case DecisionRejected:
		return VerdictRejected
	case DecisionSuperseded:
		return VerdictSuperseded
	case DecisionStale:
		return VerdictStale
	default:
		return VerdictNone
	}
}
