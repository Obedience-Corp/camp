// Package approvalkit records what a camp is waiting on a human to approve.
//
// It is published as a public camp package (the pkg/ledgerkit precedent) so
// camp and fest write the approvals ledger through one tested path.
//
// # Record and index
//
// The record is an append-only ledger in the ledgerkit format, kept at
// .campaign/approvals/<YYYY-MM>/<writer>.jsonl. Unlike the campaign event
// ledger (.campaign/events, machine-local and gitignored), this directory is
// committed: a request made on one machine has to reach the user's other
// machines, and git history should show when each request was made and
// resolved. Shards are per writer, so two machines never append to the same
// file and a pull never produces a merge conflict.
//
// The index is the set of requests still pending, cached under
// .campaign/cache/approvals/pending.json. It is derived and disposable:
// deleting it loses nothing. Load rebuilds it whenever the ledger has content
// the index has not seen, which is also how a request that arrived by git pull
// shows up.
//
// # Authority
//
// The ledger never grants anything. It is a plain file that any process can
// append to, so an approval_approved line unblocks nothing and no event
// carries a command to run. The source that is waiting (a fest workflow
// checkpoint, for example) owns its own state: its own command changes that
// state and then records the fact here. A reader that acts on a pending request
// chooses the command itself from the request's source kind.
package approvalkit

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// Dir is the approvals ledger directory, relative to the campaign root. It is
// committed, unlike ledgerkit.EventsDir.
const Dir = ".campaign/approvals"

// IndexRel is the pending-requests index, relative to the campaign root. It
// lives under the gitignored .campaign/cache.
const IndexRel = ".campaign/cache/approvals/pending.json"

// Event kinds written to the approvals ledger. They share the ledgerkit
// envelope; readers keep kinds they do not know.
const (
	KindRequested ledgerkit.Kind = "approval_requested"
	KindApproved  ledgerkit.Kind = "approval_approved"
	KindRejected  ledgerkit.Kind = "approval_rejected"
	KindWithdrawn ledgerkit.Kind = "approval_withdrawn"
)

// Outcome is how a pending request stopped being pending.
type Outcome string

const (
	// OutcomeApproved: the source's approve command succeeded.
	OutcomeApproved Outcome = "approved"
	// OutcomeRejected: the source's reject command succeeded. The source now
	// waits on whoever did the work, not on the human.
	OutcomeRejected Outcome = "rejected"
	// OutcomeWithdrawn: the request ended any other way (the step was skipped,
	// the workflow was reset or abandoned, a delegated judge decided, or a
	// reconcile found the source no longer waiting).
	OutcomeWithdrawn Outcome = "withdrawn"
)

// kind maps an outcome to its ledger kind. An unknown outcome is recorded as a
// withdrawal: the request still leaves the pending set, and nothing claims an
// approval that did not happen.
func (o Outcome) kind() ledgerkit.Kind {
	switch o {
	case OutcomeApproved:
		return KindApproved
	case OutcomeRejected:
		return KindRejected
	default:
		return KindWithdrawn
	}
}

// Source kinds a request can name. The set is open: a reader lists a kind it
// does not know and declines to act on it.
const (
	SourceFestivalCheckpoint   = "festival_checkpoint"
	SourceStandaloneCheckpoint = "standalone_checkpoint"
	SourcePhaseGate            = "phase_gate"
)

// maxBodyBytes bounds the request text stored per event so one request cannot
// bloat a committed shard.
const maxBodyBytes = 8 * 1024

// Request describes one thing waiting on a human.
type Request struct {
	// Source identifies the waiting thing across commands and machines: the
	// same workflow run or festival phase always produces the same Source. It
	// must not contain an absolute path.
	Source string
	// SourceKind selects how a reader acts on the request.
	SourceKind string
	// Step is the 1-based step the source is waiting at. Together with Source
	// it is the request's identity, and it is what an approval is pinned to.
	Step     int
	StepName string
	// Title is one line for a list. Body is the full text of what is asked.
	Title string
	Body  string
	// Cwd is the directory the source's own commands run from, relative to the
	// campaign root. Doc is the source document, also campaign-relative.
	Cwd string
	Doc string
	// Scope places the request in the campaign hierarchy.
	Scope ledgerkit.Scope
}

// Key is the request's identity: one source waits at one step at a time.
func (r Request) Key() string { return RequestKey(r.Source, r.Step) }

// RequestKey builds the identity of a request from its source and step.
func RequestKey(source string, step int) string {
	return source + "#" + strconv.Itoa(step)
}

// validate rejects a request a reader could not show or safely act on.
func (r Request) validate() error {
	if strings.TrimSpace(r.Source) == "" {
		return camperrors.New("approvalkit: request has no source")
	}
	if strings.TrimSpace(r.SourceKind) == "" {
		return camperrors.New("approvalkit: request has no source kind")
	}
	if r.Step < 1 {
		return camperrors.New("approvalkit: request step must be 1 or greater")
	}
	if strings.TrimSpace(r.Title) == "" {
		return camperrors.New("approvalkit: request has no title")
	}
	if !IsCampRelative(r.Cwd) {
		return camperrors.New("approvalkit: request cwd must be relative to the camp root: " + r.Cwd)
	}
	if r.Doc != "" && !IsCampRelative(r.Doc) {
		return camperrors.New("approvalkit: request doc must be relative to the camp root: " + r.Doc)
	}
	return nil
}

// IsCampRelative reports whether p is a relative path that stays inside the
// campaign root. "." is the root itself. It is a lexical check; ResolveCwd adds
// the symlink check a reader needs before running anything there.
func IsCampRelative(p string) bool {
	if strings.TrimSpace(p) == "" || filepath.IsAbs(p) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	return clean != ".." && !strings.HasPrefix(clean, "../")
}

// Pending is one request with no resolution yet, as a reader sees it.
type Pending struct {
	// ID is a short handle for selecting the request on a command line. EventID
	// is the full id of the approval_requested event.
	ID      string `json:"id"`
	EventID string `json:"event_id"`
	Key     string `json:"key"`

	Source     string `json:"source"`
	SourceKind string `json:"source_kind"`
	Step       int    `json:"step"`
	StepName   string `json:"step_name,omitempty"`

	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Cwd   string `json:"cwd"`
	Doc   string `json:"doc,omitempty"`

	RequestedAt string          `json:"requested_at"`
	RequestedBy ledgerkit.Actor `json:"requested_by"`
	// Writer is the ledger writer id of the machine that recorded the request.
	Writer string          `json:"writer,omitempty"`
	Scope  ledgerkit.Scope `json:"scope"`
}

// shortID derives the selectable handle from an event id. Event ids are
// UUIDv7: the leading bits are a timestamp shared by requests made close
// together, so the handle is taken from the random tail.
func shortID(eventID string) string {
	compact := strings.ReplaceAll(eventID, "-", "")
	if len(compact) > 8 {
		compact = compact[len(compact)-8:]
	}
	return "ap-" + compact
}

// Fold derives the pending set from ledger events.
//
// A request is pending when no resolution covers it. Resolutions written by
// this package carry the id of the request they resolve, and that id is what
// Fold matches on, not timestamps: a request made on one machine and resolved
// on another whose clock is behind would otherwise look resolved-before-
// requested and stay pending forever.
//
// A resolution covers the request it names and every earlier request for the
// same key. Two machines that share a source but have not yet exchanged ledgers
// can each record a request for the same checkpoint; the decision then names
// only one of them, and it settles both, because they asked the same question.
//
// events should be ordered oldest first (ledgerkit readers return them that
// way). Order decides which requests count as earlier, which unresolved request
// is shown when a key has more than one, and what a resolution that names no
// request (written by something other than this package) resolves: whatever
// was requested for its key before it.
//
// Events of other kinds, and events without a key, are ignored rather than
// rejected so a newer writer cannot break an older reader.
func Fold(events []*ledgerkit.Event) []Pending {
	resolved := make(map[string]bool)
	for _, ev := range events {
		if ev == nil || !isResolution(ev.Kind) {
			continue
		}
		if id := payloadString(ev.Payload, "request_id"); id != "" {
			resolved[id] = true
		}
	}

	// requests holds, per key, the requests not yet covered, oldest first.
	requests := make(map[string][]*ledgerkit.Event)
	for _, ev := range events {
		if ev == nil {
			continue
		}
		key := payloadString(ev.Payload, "key")
		if key == "" {
			continue
		}
		switch {
		case ev.Kind == KindRequested:
			if resolved[ev.ID] {
				// Named by a resolution: it and everything asked before it for
				// this key are settled.
				requests[key] = nil
				continue
			}
			requests[key] = append(requests[key], ev)
		case isResolution(ev.Kind):
			if payloadString(ev.Payload, "request_id") == "" {
				requests[key] = nil
			}
		}
	}

	out := make([]Pending, 0, len(requests))
	for key, open := range requests {
		if len(open) == 0 {
			continue
		}
		out = append(out, pendingFromEvent(open[len(open)-1], key))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RequestedAt != out[j].RequestedAt {
			return out[i].RequestedAt < out[j].RequestedAt
		}
		return out[i].EventID < out[j].EventID
	})
	return out
}

func isResolution(kind ledgerkit.Kind) bool {
	return kind == KindApproved || kind == KindRejected || kind == KindWithdrawn
}

func pendingFromEvent(ev *ledgerkit.Event, key string) Pending {
	return Pending{
		ID:          shortID(ev.ID),
		EventID:     ev.ID,
		Key:         key,
		Source:      payloadString(ev.Payload, "source"),
		SourceKind:  payloadString(ev.Payload, "source_kind"),
		Step:        payloadInt(ev.Payload, "step"),
		StepName:    payloadString(ev.Payload, "step_name"),
		Title:       payloadString(ev.Payload, "title"),
		Body:        payloadString(ev.Payload, "body"),
		Cwd:         payloadString(ev.Payload, "cwd"),
		Doc:         payloadString(ev.Payload, "doc"),
		RequestedAt: ev.TS,
		RequestedBy: ev.Actor,
		Writer:      payloadString(ev.Payload, "writer"),
		Scope:       ev.Scope,
	}
}

func payloadString(p map[string]any, name string) string {
	s, _ := p[name].(string)
	return s
}

// payloadInt reads a number that was an int when written and is a float64
// after a JSON round trip.
func payloadInt(p map[string]any, name string) int {
	switch v := p[name].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// Plan decides what SetWaiting has to record so that the pending set for one
// source becomes exactly want (or empty when want is nil). It is pure.
//
// resolve lists pending requests from this source that are no longer the one
// waiting; request reports whether want has to be recorded. A want that is
// already pending yields neither, which is what makes a repeated command
// harmless.
func Plan(pending []Pending, source string, want *Request) (resolve []Pending, request bool) {
	wantKey := ""
	if want != nil {
		wantKey = want.Key()
		request = true
	}
	for _, p := range pending {
		if p.Source != source {
			continue
		}
		if wantKey != "" && p.Key == wantKey {
			request = false
			continue
		}
		resolve = append(resolve, p)
	}
	return resolve, request
}

// Find selects one pending request by its short ID, its full event id, or an
// unambiguous prefix of either.
func Find(pending []Pending, selector string) (Pending, error) {
	sel := strings.TrimSpace(selector)
	if sel == "" {
		return Pending{}, camperrors.New("approvalkit: empty approval selector")
	}
	var matches []Pending
	for _, p := range pending {
		if p.ID == sel || p.EventID == sel {
			return p, nil
		}
		if strings.HasPrefix(p.ID, sel) || strings.HasPrefix(p.EventID, sel) {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return Pending{}, camperrors.NewNotFound("approval", sel, nil)
	case 1:
		return matches[0], nil
	default:
		return Pending{}, camperrors.New("approvalkit: approval selector matches more than one request: " + sel)
	}
}
