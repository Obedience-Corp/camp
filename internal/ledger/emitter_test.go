package ledger

import (
	"context"
	"errors"
	"testing"

	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureAppender records emitted events and can be made to fail, standing in
// for the real filesystem writer.
type captureAppender struct {
	events []*ledgerkit.Event
	fail   error
}

func (c *captureAppender) Emit(_ context.Context, ev *ledgerkit.Event, warn func(error)) error {
	if c.fail != nil {
		if warn != nil {
			warn(c.fail)
		}
		return c.fail
	}
	c.events = append(c.events, ev)
	return nil
}

func TestEmitStampsCampaignActionActorAndSource(t *testing.T) {
	app := &captureAppender{}
	actor := ledgerkit.Actor{Type: ledgerkit.ActorAgent, Name: "obey-agent"}
	e := newWith(app, "camp8de", actor, nil)

	e.Emit(context.Background(), ledgerkit.KindCreated, ledgerkit.Scope{Intent: "int-1"},
		WithWhy("add dark mode"), WithPayload(map[string]any{"status": "inbox"}))

	require.Len(t, app.events, 1)
	got := app.events[0]
	assert.Equal(t, ledgerkit.EnvelopeVersion, got.V)
	assert.NotEmpty(t, got.ID)
	assert.NotEmpty(t, got.TS)
	assert.Equal(t, ledgerkit.KindCreated, got.Kind)
	assert.Equal(t, "camp8de", got.Scope.Campaign, "campaign id is stamped")
	assert.Equal(t, "int-1", got.Scope.Intent)
	assert.Equal(t, e.ActionID(), got.Action, "the invocation action id is stamped")
	assert.Equal(t, actor, got.Actor)
	assert.Equal(t, ledgerkit.SourceCommand, got.Source)
	assert.Equal(t, "add dark mode", got.Why)
	assert.Equal(t, "inbox", got.Payload["status"])
}

func TestEmitSharedActionIDAcrossEvents(t *testing.T) {
	app := &captureAppender{}
	e := newWith(app, "c", ledgerkit.Actor{Type: ledgerkit.ActorHuman}, nil)
	e.Emit(context.Background(), ledgerkit.KindCreated, ledgerkit.Scope{Workitem: "w1"})
	e.Emit(context.Background(), ledgerkit.KindEvidenceAttached, ledgerkit.Scope{Workitem: "w1"})
	require.Len(t, app.events, 2)
	assert.Equal(t, app.events[0].Action, app.events[1].Action,
		"a root+submodule style multi-event action shares one action id (D002)")
}

// TestEmitFailureNeverBlocksCaller proves D003: a failing appender warns but the
// caller's flow proceeds and Emit does not panic or propagate a fatal error.
func TestEmitFailureNeverBlocksCaller(t *testing.T) {
	app := &captureAppender{fail: errors.New("disk full")}
	var warned error
	e := newWith(app, "c", ledgerkit.Actor{Type: ledgerkit.ActorHuman}, func(err error) { warned = err })

	stateChanged := false
	// The command pattern: apply the state change, then emit.
	stateChanged = true
	e.Emit(context.Background(), ledgerkit.KindCreated, ledgerkit.Scope{Intent: "i"})

	assert.True(t, stateChanged, "state change completes despite emission failure")
	require.Error(t, warned)
	assert.Contains(t, warned.Error(), "disk full")
}

func TestDisabledEmitterIsSilentNoOp(t *testing.T) {
	// New with an empty campaign root disables the emitter without a writer.
	e := New(context.Background(), "", "", func(error) { t.Fatal("disabled emitter must not warn on emit") })
	assert.True(t, e.disabled)
	// Emitting is a no-op and must not panic.
	e.Emit(context.Background(), ledgerkit.KindCreated, ledgerkit.Scope{})
}

func TestAddExplicitFailsClosedOnWriteError(t *testing.T) {
	app := &captureAppender{fail: errors.New("disk full")}
	e := newWith(app, "c", ledgerkit.Actor{Type: ledgerkit.ActorHuman}, nil)
	e.campaignRoot = "/tmp/camp"
	e.writerID = "test"
	id, shard, err := e.AddExplicit(context.Background(), ledgerkit.KindDecided, ledgerkit.Scope{})
	require.Error(t, err)
	assert.Empty(t, id, "must not return a fake event_id when the write failed")
	assert.Empty(t, shard)
}

func TestAddExplicitReturnsIDOnSuccess(t *testing.T) {
	app := &captureAppender{}
	e := newWith(app, "c", ledgerkit.Actor{Type: ledgerkit.ActorHuman}, nil)
	e.campaignRoot = "/tmp/camp"
	e.writerID = "test"
	id, shard, err := e.AddExplicit(context.Background(), ledgerkit.KindDecided, ledgerkit.Scope{}, WithWhy("note"))
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.NotEmpty(t, shard)
	require.Len(t, app.events, 1)
	assert.Equal(t, ledgerkit.SourceExplicit, app.events[0].Source)
}

func TestWarnToWritesLoudly(t *testing.T) {
	var buf testWriter
	warn := WarnTo(&buf)
	warn(errors.New("boom"))
	assert.Contains(t, buf.String(), "campaign ledger not updated")
	assert.Contains(t, buf.String(), "boom")
	warn(nil) // nil is ignored
}

type testWriter struct{ b []byte }

func (w *testWriter) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }
func (w *testWriter) String() string              { return string(w.b) }
