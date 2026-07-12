// Package ledger adapts the shared pkg/ledgerkit writer for camp command use:
// it resolves the campaign, writer id, and actor once per invocation and lets a
// state-changing command emit a ledger event in ~3 lines. Emission is
// best-effort-but-loud (D003): a ledger problem prints a warning but never fails
// the command's state change.
package ledger

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// Emitter emits ledger events for one command invocation, stamping a shared
// action id, the resolved actor, and the campaign id onto every event so a
// root+submodule action is grouped (D002) and scope is consistent.
type Emitter struct {
	writer       ledgerAppender
	campaignID   string
	campaignRoot string
	writerID     string
	actionID     string
	actor        ledgerkit.Actor
	warn         func(error)
	disabled     bool
}

// ledgerAppender is the subset of *ledgerkit.Writer the emitter needs, so tests
// inject a fake without a real filesystem writer.
type ledgerAppender interface {
	Emit(ctx context.Context, ev *ledgerkit.Event, warn func(error)) error
}

// New resolves the writer, actor, and a fresh action id for a command emitting
// against the campaign at campaignRoot (id campaignID). Resolution failure is
// non-fatal: it returns a disabled emitter that warns once, so a command never
// fails because the ledger could not be initialised (D003). warn prints the
// loud warning; pass WarnTo(cmd.ErrOrStderr()).
func New(ctx context.Context, campaignRoot, campaignID string, warn func(error)) *Emitter {
	if warn == nil {
		warn = func(error) {}
	}
	e := &Emitter{
		campaignID: campaignID,
		actionID:   ledgerkit.NewActionID(),
		actor:      resolveActor(ctx),
		warn:       warn,
	}
	if campaignRoot == "" || campaignID == "" {
		e.disabled = true
		return e
	}
	writerID, err := ledgerkit.ResolveWriterID(ctx)
	if err != nil {
		warn(camperrors.Wrap(err, "ledger: resolve writer id"))
		e.disabled = true
		return e
	}
	w, err := ledgerkit.NewWriter(campaignRoot, writerID)
	if err != nil {
		warn(camperrors.Wrap(err, "ledger: init writer"))
		e.disabled = true
		return e
	}
	e.writer = w
	e.campaignRoot = campaignRoot
	e.writerID = writerID
	return e
}

// AddExplicit emits an explicit (source: explicit) event - the camp event add
// primitive for actions that never touch git - and returns the created event id
// and the shard file it landed in, for the command to report. Returns empty
// strings when the emitter is disabled.
func (e *Emitter) AddExplicit(ctx context.Context, kind ledgerkit.Kind, scope ledgerkit.Scope, opts ...Option) (eventID, shardPath string) {
	if e.disabled || e.writer == nil {
		return "", ""
	}
	scope.Campaign = e.campaignID
	now := time.Now()
	ev := &ledgerkit.Event{
		V:      ledgerkit.EnvelopeVersion,
		ID:     ledgerkit.NewEventID(),
		TS:     ledgerkit.NowUTC(now),
		Kind:   kind,
		Scope:  scope,
		Action: e.actionID,
		Actor:  e.actor,
		Source: ledgerkit.SourceExplicit,
	}
	for _, o := range opts {
		o(ev)
	}
	_ = e.writer.Emit(ctx, ev, e.warn)
	return ev.ID, ledgerkit.ShardPath(e.campaignRoot, e.writerID, now)
}

// NewFromRoot builds an emitter for the campaign rooted at campaignRoot,
// loading the campaign id from config. It lets internal services self-instrument
// from just the root they already hold, with no command-layer wiring. Config
// failure disables emission (non-fatal, D003).
func NewFromRoot(ctx context.Context, campaignRoot string, warn func(error)) *Emitter {
	if warn == nil {
		warn = WarnToStderr()
	}
	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		warn(camperrors.Wrap(err, "ledger: load campaign id"))
		return &Emitter{disabled: true, warn: warn, actionID: ledgerkit.NewActionID()}
	}
	return New(ctx, campaignRoot, cfg.ID, warn)
}

// WarnToStderr returns a warn function that prints to os.Stderr, for callers
// (internal services) without a cobra command handle.
func WarnToStderr() func(error) { return WarnTo(os.Stderr) }

// newWith builds an emitter around an injected appender (test seam).
func newWith(writer ledgerAppender, campaignID string, actor ledgerkit.Actor, warn func(error)) *Emitter {
	if warn == nil {
		warn = func(error) {}
	}
	return &Emitter{writer: writer, campaignID: campaignID, actionID: ledgerkit.NewActionID(), actor: actor, warn: warn}
}

// ActionID is the shared action id stamped on this invocation's events, exposed
// so a caller can thread it into related evidence (D002 action identity).
func (e *Emitter) ActionID() string { return e.actionID }

// Option customises an event before it is appended.
type Option func(*ledgerkit.Event)

// WithWhy sets the human-facing reason (defaulted from the command's own
// message where one exists, D005).
func WithWhy(why string) Option { return func(ev *ledgerkit.Event) { ev.Why = why } }

// WithPayload sets the kind-specific payload (free-form, D001).
func WithPayload(p map[string]any) Option { return func(ev *ledgerkit.Event) { ev.Payload = p } }

// WithEvidence attaches typed evidence references.
func WithEvidence(refs ...ledgerkit.Evidence) Option {
	return func(ev *ledgerkit.Event) { ev.Evidence = append(ev.Evidence, refs...) }
}

// WithAction overrides the invocation action id (e.g. to join an ongoing
// action). Default is the emitter's per-invocation id.
func WithAction(id string) Option { return func(ev *ledgerkit.Event) { ev.Action = id } }

// Emit appends one event of the given kind and scope, stamping campaign, action,
// actor, ts, and source. It is best-effort: failure warns but never returns to
// the caller's control flow (D003). A disabled emitter is a silent no-op (the
// disabling reason was already warned once).
func (e *Emitter) Emit(ctx context.Context, kind ledgerkit.Kind, scope ledgerkit.Scope, opts ...Option) {
	if e.disabled || e.writer == nil {
		return
	}
	scope.Campaign = e.campaignID
	ev := &ledgerkit.Event{
		V:      ledgerkit.EnvelopeVersion,
		ID:     ledgerkit.NewEventID(),
		TS:     ledgerkit.NowUTC(time.Now()),
		Kind:   kind,
		Scope:  scope,
		Action: e.actionID,
		Actor:  e.actor,
		Source: ledgerkit.SourceCommand,
	}
	for _, o := range opts {
		o(ev)
	}
	_ = e.writer.Emit(ctx, ev, e.warn)
}

// WarnTo returns a warn function that prints ledger emission problems to w,
// loudly but without failing the command (D003).
func WarnTo(w io.Writer) func(error) {
	return func(err error) {
		if err == nil {
			return
		}
		_, _ = fmt.Fprintf(w, "warning: campaign ledger not updated: %v\n", err)
	}
}

// resolveActor determines who ran the command. Session is an opaque passthrough
// from obey when present (D009 Q11); its presence, or a known agent marker,
// classifies the actor as an agent, else human.
func resolveActor(ctx context.Context) ledgerkit.Actor {
	actor := ledgerkit.Actor{Type: ledgerkit.ActorHuman}
	if name := git.GetUserName(ctx); name != "" {
		actor.Name = name
	} else if u := os.Getenv("USER"); u != "" {
		actor.Name = u
	}
	if session := os.Getenv("OBEY_SESSION"); session != "" {
		actor.Session = session
		actor.Type = ledgerkit.ActorAgent
	} else if isAgentEnv() {
		actor.Type = ledgerkit.ActorAgent
	}
	return actor
}

// isAgentEnv reports whether the process appears to be an agent/automation run.
func isAgentEnv() bool {
	for _, key := range []string{"CLAUDECODE", "OBEY_AGENT", "CI"} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}
