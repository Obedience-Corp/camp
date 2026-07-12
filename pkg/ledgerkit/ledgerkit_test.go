package ledgerkit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memOpener is an injected shardOpener that captures appended bytes per path in
// memory, so writer logic is tested without host filesystem mutation.
type memOpener struct {
	shards map[string]*bytes.Buffer
}

func newMemOpener() *memOpener { return &memOpener{shards: map[string]*bytes.Buffer{}} }

func (m *memOpener) open(path string) (io.WriteCloser, error) {
	buf, ok := m.shards[path]
	if !ok {
		buf = &bytes.Buffer{}
		m.shards[path] = buf
	}
	return nopWriteCloser{buf}, nil
}

type nopWriteCloser struct{ w io.Writer }

func (n nopWriteCloser) Write(p []byte) (int, error) { return n.w.Write(p) }
func (n nopWriteCloser) Close() error                { return nil }

func sampleEvent(id, ts string, kind Kind) *Event {
	return &Event{
		V:      EnvelopeVersion,
		ID:     id,
		TS:     ts,
		Kind:   kind,
		Scope:  Scope{Campaign: "8deed8b4", Festival: "CA0002"},
		Actor:  Actor{Type: ActorAgent, Name: "obey-agent"},
		Source: SourceCommand,
	}
}

func TestParseShard(t *testing.T) {
	tests := []struct {
		name          string
		line          string
		wantSkipped   bool
		wantReason    string
		wantEvents    int
		wantUnknownV  int
		assertOnEvent func(t *testing.T, ev *Event)
	}{
		{name: "invalid json is skipped and reported", line: `{not json`, wantSkipped: true, wantReason: "invalid json", wantEvents: 0},
		{name: "missing id is skipped", line: `{"v":1,"ts":"2026-07-11T00:00:00Z","kind":"created"}`, wantSkipped: true, wantReason: "missing id", wantEvents: 0},
		{name: "missing ts is skipped", line: `{"v":1,"id":"e1","kind":"created"}`, wantSkipped: true, wantReason: "missing ts", wantEvents: 0},
		{
			name:       "valid event parses",
			line:       `{"v":1,"id":"e1","ts":"2026-07-11T00:00:00Z","kind":"created","scope":{"campaign":"c"},"actor":{"type":"human"},"source":"command"}`,
			wantEvents: 1,
			assertOnEvent: func(t *testing.T, ev *Event) {
				assert.Equal(t, "e1", ev.ID)
				assert.Equal(t, KindCreated, ev.Kind)
			},
		},
		{
			name:       "unknown top-level field is tolerated",
			line:       `{"v":1,"id":"e2","ts":"2026-07-11T00:00:00Z","kind":"created","actor":{"type":"human"},"source":"command","future_field":42}`,
			wantEvents: 1,
		},
		{
			name:       "unknown kind is kept",
			line:       `{"v":1,"id":"e3","ts":"2026-07-11T00:00:00Z","kind":"teleported","actor":{"type":"human"},"source":"command"}`,
			wantEvents: 1,
			assertOnEvent: func(t *testing.T, ev *Event) {
				assert.Equal(t, Kind("teleported"), ev.Kind)
			},
		},
		{
			name:         "unknown envelope version is kept but counted",
			line:         `{"v":99,"id":"e4","ts":"2026-07-11T00:00:00Z","kind":"created","actor":{"type":"human"},"source":"command"}`,
			wantEvents:   1,
			wantUnknownV: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, skipped, unknownV := ParseShard("shard.jsonl", strings.NewReader(tt.line+"\n"))
			require.Len(t, events, tt.wantEvents)
			assert.Equal(t, tt.wantUnknownV, unknownV)
			if tt.wantSkipped {
				require.Len(t, skipped, 1)
				assert.Contains(t, skipped[0].Reason, tt.wantReason)
				assert.Equal(t, "shard.jsonl", skipped[0].Shard)
				assert.Equal(t, 1, skipped[0].Line)
			} else {
				assert.Empty(t, skipped)
			}
			if tt.assertOnEvent != nil && len(events) == 1 {
				tt.assertOnEvent(t, events[0])
			}
		})
	}
}

func TestParseShardBlankLinesIgnored(t *testing.T) {
	events, skipped, _ := ParseShard("s", strings.NewReader("\n   \n\t\n"))
	assert.Empty(t, events)
	assert.Empty(t, skipped)
}

func TestMergeOrderingAndDedupe(t *testing.T) {
	// Two shards, out of order, with one duplicate id across shards.
	a := []*Event{
		sampleEvent("id-c", "2026-07-11T03:00:00Z", KindCreated),
		sampleEvent("id-a", "2026-07-11T01:00:00Z", KindCreated),
	}
	b := []*Event{
		sampleEvent("id-b", "2026-07-11T02:00:00Z", KindCreated),
		sampleEvent("id-a", "2026-07-11T01:00:00Z", KindCreated), // duplicate id
	}
	merged, err := Merge(context.Background(), [][]*Event{a, b})
	require.NoError(t, err)
	require.Len(t, merged, 3, "duplicate id must be collapsed")
	assert.Equal(t, []string{"id-a", "id-b", "id-c"}, []string{merged[0].ID, merged[1].ID, merged[2].ID})
}

func TestMergeTieBreaksByID(t *testing.T) {
	ts := "2026-07-11T01:00:00Z"
	merged, err := Merge(context.Background(), [][]*Event{{
		sampleEvent("id-z", ts, KindCreated),
		sampleEvent("id-a", ts, KindCreated),
	}})
	require.NoError(t, err)
	assert.Equal(t, "id-a", merged[0].ID, "equal ts sorts by id")
	assert.Equal(t, "id-z", merged[1].ID)
}

func TestMergeCoarseAndBadTimestampsTolerated(t *testing.T) {
	merged, err := Merge(context.Background(), [][]*Event{{
		sampleEvent("id-bad", "not-a-time", KindCreated),
		sampleEvent("id-ok", "2026-07-11T01:00:00Z", KindCreated),
	}})
	require.NoError(t, err)
	require.Len(t, merged, 2, "unparseable ts is retained, not dropped")
	assert.Equal(t, "id-ok", merged[0].ID, "valid ts sorts before unparseable")
}

func TestMergeContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Merge(ctx, [][]*Event{{sampleEvent("id", "2026-07-11T00:00:00Z", KindCreated)}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestShardPathTwoWritersDistinct(t *testing.T) {
	ts := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	pathA := ShardPath("/camp", "wAAAA", ts)
	pathB := ShardPath("/camp", "wBBBB", ts)
	assert.Equal(t, "/camp/.campaign/events/2026-07/wAAAA.jsonl", pathA)
	assert.NotEqual(t, pathA, pathB, "two writers never share a shard file in a month (D002 conflict-free)")
	// Month bucket comes from the event timestamp, in UTC.
	dec := ShardPath("/camp", "wAAAA", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
	assert.Contains(t, dec, "/2026-12/")
}

func TestDerivedIDDeterministic(t *testing.T) {
	a := DerivedID("bf", "commit", "camp", "abc123")
	b := DerivedID("bf", "commit", "camp", "abc123")
	c := DerivedID("bf", "commit", "camp", "def456")
	assert.Equal(t, a, b, "same source identity yields the same id (idempotent backfill)")
	assert.NotEqual(t, a, c, "different source identity yields a different id")
	assert.True(t, strings.HasPrefix(a, "bf_"), "derived ids are prefixed so they never collide with a UUIDv7")
}

func TestNewEventIDUniqueAndFormatted(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 1000; i++ {
		id := NewEventID()
		require.NotEmpty(t, id)
		_, dup := seen[id]
		require.False(t, dup, "event ids must be unique")
		seen[id] = struct{}{}
	}
}

func TestEncodeEventRoundTrip(t *testing.T) {
	ev := sampleEvent("e1", "2026-07-11T00:00:00Z", KindEvidenceAttached)
	ev.Action = "act1"
	ev.Why = "why it happened"
	ev.Payload = map[string]any{"from": "ready", "to": "active"}
	ev.Evidence = []Evidence{{Type: EvidenceCommit, Repo: "camp", SHA: "deadbeef"}}
	line, err := encodeEvent(ev)
	require.NoError(t, err)
	require.True(t, bytes.HasSuffix(line, []byte("\n")), "each event is a newline-terminated line")

	var got Event
	require.NoError(t, json.Unmarshal(line, &got))
	assert.Equal(t, ev.ID, got.ID)
	assert.Equal(t, ev.Kind, got.Kind)
	assert.Equal(t, ev.Action, got.Action)
	assert.Equal(t, ev.Evidence, got.Evidence)
	assert.Equal(t, "ready", got.Payload["from"])
}

func TestWriterAppendRoutesAndEncodes(t *testing.T) {
	mem := newMemOpener()
	w := &Writer{campaignRoot: "/camp", writerID: "wTEST", open: mem.open}
	ev := sampleEvent("e1", "2026-07-11T09:00:00Z", KindCreated)
	require.NoError(t, w.Append(context.Background(), ev))

	wantPath := "/camp/.campaign/events/2026-07/wTEST.jsonl"
	buf, ok := mem.shards[wantPath]
	require.True(t, ok, "event routed to the month/writer shard")
	var got Event
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &got))
	assert.Equal(t, "e1", got.ID)
}

func TestWriterAppendErrors(t *testing.T) {
	mem := newMemOpener()
	w := &Writer{campaignRoot: "/camp", writerID: "wTEST", open: mem.open}

	t.Run("nil event", func(t *testing.T) {
		require.Error(t, w.Append(context.Background(), nil))
	})
	t.Run("empty id", func(t *testing.T) {
		require.Error(t, w.Append(context.Background(), sampleEvent("", "2026-07-11T00:00:00Z", KindCreated)))
	})
	t.Run("cancelled context does not write", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := w.Append(ctx, sampleEvent("e2", "2026-07-11T00:00:00Z", KindCreated))
		require.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, mem.shards, "no shard opened when context is already cancelled")
	})
}

// TestEmitCannotBlockCaller proves D003: a failed append is loud (warn invoked)
// but never propagates as a fatal error that would stop the caller's state
// change. The simulated caller ignores Emit's return and completes regardless.
func TestEmitCannotBlockCaller(t *testing.T) {
	failing := func(string) (io.WriteCloser, error) {
		return nil, io.ErrClosedPipe // stand-in for a disk failure
	}
	w := &Writer{campaignRoot: "/camp", writerID: "wTEST", open: failing}

	var warned error
	stateChangeApplied := false

	// The caller pattern used by every state-changing command: do the state
	// change, then emit best-effort, ignoring the emit result for control flow.
	applyStateChange := func() {
		stateChangeApplied = true
		_ = w.Emit(context.Background(), sampleEvent("e1", "2026-07-11T00:00:00Z", KindCreated), func(err error) {
			warned = err
		})
	}
	applyStateChange()

	assert.True(t, stateChangeApplied, "state change completes despite emission failure")
	require.Error(t, warned, "emission failure is surfaced loudly via warn")
}

// TestTwoWriterMergeConflictFree proves D002: two machines writing the same
// campaign in the same month land in distinct shard files (no shared file to
// conflict on), and a reader merges both into one correctly ordered stream.
func TestTwoWriterMergeConflictFree(t *testing.T) {
	memA := newMemOpener()
	memB := newMemOpener()
	wA := &Writer{campaignRoot: "/camp", writerID: "wAAAA", open: memA.open}
	wB := &Writer{campaignRoot: "/camp", writerID: "wBBBB", open: memB.open}

	require.NoError(t, wA.Append(context.Background(), sampleEvent("id-a1", "2026-07-11T01:00:00Z", KindCreated)))
	require.NoError(t, wB.Append(context.Background(), sampleEvent("id-b1", "2026-07-11T02:00:00Z", KindCreated)))

	pathA := "/camp/.campaign/events/2026-07/wAAAA.jsonl"
	pathB := "/camp/.campaign/events/2026-07/wBBBB.jsonl"
	require.Contains(t, memA.shards, pathA)
	require.Contains(t, memB.shards, pathB)
	require.NotEqual(t, pathA, pathB, "distinct shard files: cross-writer merges are file-adds git resolves cleanly")

	shardA, _, _ := ParseShard(pathA, strings.NewReader(memA.shards[pathA].String()))
	shardB, _, _ := ParseShard(pathB, strings.NewReader(memB.shards[pathB].String()))
	merged, err := Merge(context.Background(), [][]*Event{shardA, shardB})
	require.NoError(t, err)
	require.Len(t, merged, 2)
	assert.Equal(t, "id-a1", merged[0].ID)
	assert.Equal(t, "id-b1", merged[1].ID)
}
