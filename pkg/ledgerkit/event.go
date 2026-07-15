// Package ledgerkit is the shared campaign event-ledger writer/reader,
// published as a public camp package (the pkg/commitkit precedent) so camp
// commands, fest, and future tools emit through one tested path without
// importing camp internals.
//
// The ledger is an append-only stream of JSON-per-line events under
// .campaign/events/<YYYY-MM>/<writer>.jsonl (decision D002). Each event is the
// D001 envelope. The ledger is files+git only: no database, no daemon; git
// history is the transition record and any derived store is a disposable index.
//
// Event and action ids are UUIDv7 (google/uuid, already a camp dependency).
// UUIDv7 is time-ordered and lexicographically sortable with a random tail, so
// it fulfils the "ULID" role D001/D002 specify (sort-stable, collision-safe
// across writers) without adding a third-party ULID library. Backfill and
// reconciled events instead carry a deterministic id derived from source
// identity so re-runs converge (D001 finalization); those ids are prefixed so
// they never collide with a live UUIDv7.
package ledgerkit

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EnvelopeVersion is the current envelope version written by this library.
// Readers tolerate other versions (unknown fields are ignored, unknown kinds
// are kept and surfaced to the doctor).
const EnvelopeVersion = 1

// Kind is the small, closed-ish set of event kinds (D001). Readers keep
// unknown kinds rather than rejecting them.
type Kind string

const (
	KindCreated          Kind = "created"
	KindTransitioned     Kind = "transitioned"
	KindCompleted        Kind = "completed"
	KindDecided          Kind = "decided"
	KindEvidenceAttached Kind = "evidence_attached"
	KindReconciled       Kind = "reconciled"
	KindRepaired         Kind = "repaired"
	KindClaimed          Kind = "claimed"
	KindReleased         Kind = "released"
)

// Source records how an event entered the ledger (D001).
type Source string

const (
	SourceCommand    Source = "command"    // a state-changing CLI verb emitted it
	SourceExplicit   Source = "explicit"   // camp event add
	SourceReconciled Source = "reconciled" // doctor reconciliation
	SourceBackfill   Source = "backfill"   // derived from pre-ledger history
)

// ActorType classifies who acted. unknown is required because historical
// sources (status histories) carry no actor and commit authors are only
// heuristically classifiable (D001 finalization).
type ActorType string

const (
	ActorHuman   ActorType = "human"
	ActorAgent   ActorType = "agent"
	ActorUnknown ActorType = "unknown"
)

// EvidenceType enumerates the typed evidence references an event may carry.
type EvidenceType string

const (
	EvidenceCommit EvidenceType = "commit"
	EvidencePath   EvidenceType = "path"
	EvidenceURL    EvidenceType = "url"
)

// Scope holds the parent pointers that place an event in the campaign
// hierarchy. Campaign is always set; the rest are set when applicable and
// drive zoom levels and lane/node assignment (D007/D008). Festival is the
// canonical fest id (e.g. CA0002), never a directory name (D001 finalization).
type Scope struct {
	Campaign string `json:"campaign"`
	Festival string `json:"festival,omitempty"`
	Phase    string `json:"phase,omitempty"`
	Sequence string `json:"sequence,omitempty"`
	Task     string `json:"task,omitempty"`
	Workitem string `json:"workitem,omitempty"`
	Intent   string `json:"intent,omitempty"`
	Quest    string `json:"quest,omitempty"`
}

// Actor is who performed the action. Session is reserved for obey and is never
// required (D009 Q11).
type Actor struct {
	Type    ActorType `json:"type"`
	Name    string    `json:"name,omitempty"`
	Session string    `json:"session,omitempty"`
}

// Evidence is a typed reference to a produced artifact. Fields are populated
// per Type: commit -> Repo+SHA; path -> Path; url -> URL.
type Evidence struct {
	Type EvidenceType `json:"type"`
	Repo string       `json:"repo,omitempty"`
	SHA  string       `json:"sha,omitempty"`
	Path string       `json:"path,omitempty"`
	URL  string       `json:"url,omitempty"`
}

// Event is the D001 envelope: one JSON object per ledger line.
type Event struct {
	V        int            `json:"v"`
	ID       string         `json:"id"`
	TS       string         `json:"ts"` // RFC3339, UTC
	Kind     Kind           `json:"kind"`
	Scope    Scope          `json:"scope"`
	Action   string         `json:"action,omitempty"`
	Actor    Actor          `json:"actor"`
	Why      string         `json:"why,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
	Evidence []Evidence     `json:"evidence,omitempty"`
	Source   Source         `json:"source"`
}

// NewEventID returns a fresh time-ordered, sortable event id (UUIDv7) for live
// capture. On the astronomically unlikely RNG failure it falls back to a v4 so
// callers never receive an empty id.
func NewEventID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return uuid.NewString()
}

// NewActionID returns a fresh action id (UUIDv7). A wrapper command generates
// one per invocation and stamps it into every event and evidence ref it
// produces so a root commit plus its submodule commits are one action (D002).
func NewActionID() string { return NewEventID() }

// DerivedID returns a deterministic id for backfill/reconciled events, derived
// from source identity so re-runs converge (D001 finalization). prefix marks
// the source class ("bf" backfill, "rc" reconciled) so derived ids never
// collide with a live UUIDv7.
func DerivedID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "_" + hex.EncodeToString(sum[:])[:24]
}

// NowUTC formats t as the ledger's canonical timestamp: RFC3339 with
// nanoseconds, in UTC. Capture uses this; backfill normalizes source offsets to
// the same shape (D001: ts is RFC3339 UTC).
func NowUTC(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
