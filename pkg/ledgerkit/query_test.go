package ledgerkit

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memStore is an in-memory shardStore so Query/ReadSince are tested without
// host filesystem mutation (the repo's no-host-fs-tests rule).
type memStore struct {
	shards map[string][]byte
	reads  map[string]int // readFrom call count per shard, for pushdown assertions
}

func newMemStore() *memStore {
	return &memStore{shards: map[string][]byte{}, reads: map[string]int{}}
}

func shardRel(month, writer string) string {
	return filepath.Join(EventsDir, month, writer+".jsonl")
}

func (m *memStore) put(month, writer string, lines ...string) {
	rel := shardRel(month, writer)
	for _, ln := range lines {
		m.shards[rel] = append(m.shards[rel], []byte(ln+"\n")...)
	}
}

func (m *memStore) list() ([]shardRef, error) {
	rels := make([]string, 0, len(m.shards))
	for rel := range m.shards {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	refs := make([]shardRef, 0, len(rels))
	for _, rel := range rels {
		refs = append(refs, shardRef{Rel: rel, Month: filepath.Base(filepath.Dir(rel))})
	}
	return refs, nil
}

func (m *memStore) size(rel string) (int64, error) { return int64(len(m.shards[rel])), nil }

func (m *memStore) readFrom(rel string, offset int64) ([]byte, error) {
	m.reads[rel]++
	data := m.shards[rel]
	if offset >= int64(len(data)) {
		return nil, nil
	}
	return data[offset:], nil
}

func readerOn(store shardStore) *Reader { return &Reader{store: store} }

func eventLine(t *testing.T, ev *Event) string {
	t.Helper()
	line, err := encodeEvent(ev)
	require.NoError(t, err)
	return string(line[:len(line)-1]) // drop trailing newline; put re-adds it
}

func scopedEvent(id, ts string, kind Kind, sc Scope, src Source) *Event {
	return &Event{V: EnvelopeVersion, ID: id, TS: ts, Kind: kind, Scope: sc, Actor: Actor{Type: ActorAgent}, Source: src}
}

func TestFilterMatches(t *testing.T) {
	base := scopedEvent("e", "2026-07-11T00:00:00Z", KindCreated,
		Scope{Campaign: "c1", Festival: "CA0002", Workitem: "wi-1"}, SourceCommand)
	tests := []struct {
		name string
		f    Filter
		want bool
	}{
		{"empty filter matches", Filter{}, true},
		{"campaign match", Filter{Campaign: "c1"}, true},
		{"campaign mismatch", Filter{Campaign: "other"}, false},
		{"festival match", Filter{Festival: "CA0002"}, true},
		{"festival mismatch", Filter{Festival: "CX9999"}, false},
		{"workitem match", Filter{Workitem: "wi-1"}, true},
		{"kind in set", Filter{Kinds: []Kind{KindCreated, KindCompleted}}, true},
		{"kind not in set", Filter{Kinds: []Kind{KindCompleted}}, false},
		{"source in set", Filter{Sources: []Source{SourceCommand}}, true},
		{"source not in set", Filter{Sources: []Source{SourceBackfill}}, false},
		{"within time window", Filter{Since: mustTime("2026-07-01T00:00:00Z"), Until: mustTime("2026-07-31T00:00:00Z")}, true},
		{"before since", Filter{Since: mustTime("2026-08-01T00:00:00Z")}, false},
		{"after until", Filter{Until: mustTime("2026-07-01T00:00:00Z")}, false},
		{"combined AND all match", Filter{Campaign: "c1", Kinds: []Kind{KindCreated}}, true},
		{"combined AND one fails", Filter{Campaign: "c1", Kinds: []Kind{KindCompleted}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.f.Matches(base))
		})
	}
}

func TestQueryFiltersAndMonthPushdown(t *testing.T) {
	store := newMemStore()
	store.put("2026-06", "wA", eventLine(t, scopedEvent("jun-1", "2026-06-15T00:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)))
	store.put("2026-07", "wA",
		eventLine(t, scopedEvent("jul-1", "2026-07-10T00:00:00Z", KindCreated, Scope{Campaign: "c1", Festival: "CA0002"}, SourceCommand)),
		eventLine(t, scopedEvent("jul-2", "2026-07-11T00:00:00Z", KindCompleted, Scope{Campaign: "c1", Festival: "CA0002"}, SourceCommand)),
	)
	r := readerOn(store)

	// Time window covering only July: the June shard must be skipped unopened.
	got, report, err := r.Query(context.Background(), Filter{
		Since: mustTime("2026-07-01T00:00:00Z"),
		Until: mustTime("2026-07-31T23:59:59Z"),
	})
	require.NoError(t, err)
	require.Empty(t, report.Skipped)
	require.Len(t, got, 2)
	assert.Equal(t, 0, store.reads[shardRel("2026-06", "wA")], "June shard skipped by month pushdown")
	assert.Equal(t, 1, store.reads[shardRel("2026-07", "wA")], "July shard read once")

	// Kind filter within the window.
	got, _, err = r.Query(context.Background(), Filter{Kinds: []Kind{KindCompleted}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "jul-2", got[0].ID)
}

func TestCursorEncodeDecodeRoundTrip(t *testing.T) {
	c := Cursor{Version: cursorVersion, Offsets: map[string]int64{"a.jsonl": 10, "b.jsonl": 20}}
	tok, err := c.Encode()
	require.NoError(t, err)
	got, err := DecodeCursor(tok)
	require.NoError(t, err)
	assert.Equal(t, c.Offsets, got.Offsets)

	empty, err := DecodeCursor("")
	require.NoError(t, err)
	assert.Empty(t, empty.Offsets, "empty token reads from the beginning")

	_, err = DecodeCursor("!!!not-base64!!!")
	require.Error(t, err)
}

func TestReadSinceIncremental(t *testing.T) {
	store := newMemStore()
	rel := shardRel("2026-07", "wA")
	store.put("2026-07", "wA", eventLine(t, scopedEvent("e1", "2026-07-11T01:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)))
	r := readerOn(store)

	// First read from the zero cursor returns everything.
	res, err := r.ReadSince(context.Background(), Cursor{})
	require.NoError(t, err)
	require.Len(t, res.Events, 1)
	assert.Equal(t, "e1", res.Events[0].ID)
	firstOffset := res.Next.Offsets[rel]
	assert.Equal(t, int64(len(store.shards[rel])), firstOffset)

	// Appending and reading with the returned cursor yields only the new event.
	store.put("2026-07", "wA", eventLine(t, scopedEvent("e2", "2026-07-11T02:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)))
	res2, err := r.ReadSince(context.Background(), res.Next)
	require.NoError(t, err)
	require.Len(t, res2.Events, 1, "only the newly appended event is returned")
	assert.Equal(t, "e2", res2.Events[0].ID)

	// Re-reading with the latest cursor yields nothing new.
	res3, err := r.ReadSince(context.Background(), res2.Next)
	require.NoError(t, err)
	assert.Empty(t, res3.Events)
}

func TestReadSinceNewMonthAndNewWriter(t *testing.T) {
	store := newMemStore()
	store.put("2026-07", "wA", eventLine(t, scopedEvent("a1", "2026-07-11T01:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)))
	r := readerOn(store)
	res, err := r.ReadSince(context.Background(), Cursor{})
	require.NoError(t, err)
	require.Len(t, res.Events, 1)

	// A month rotation (new month shard) and a second machine (new writer shard)
	// both appear as shards absent from the cursor and are read from offset 0.
	store.put("2026-08", "wA", eventLine(t, scopedEvent("a2", "2026-08-01T00:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)))
	store.put("2026-07", "wB", eventLine(t, scopedEvent("b1", "2026-07-12T00:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)))

	res2, err := r.ReadSince(context.Background(), res.Next)
	require.NoError(t, err)
	ids := []string{res2.Events[0].ID, res2.Events[1].ID}
	sort.Strings(ids)
	assert.Equal(t, []string{"a2", "b1"}, ids, "new month and new writer shards are picked up from the start")
}

func TestReadSinceShardViolationReReads(t *testing.T) {
	store := newMemStore()
	rel := shardRel("2026-07", "wA")
	store.put("2026-07", "wA",
		eventLine(t, scopedEvent("e1", "2026-07-11T01:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)),
		eventLine(t, scopedEvent("e2", "2026-07-11T02:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)),
	)
	r := readerOn(store)
	res, err := r.ReadSince(context.Background(), Cursor{})
	require.NoError(t, err)
	require.Len(t, res.Events, 2)

	// Simulate an append-only violation: the shard is rewritten shorter than the
	// recorded offset. ReadSince must reset to 0, re-read fully, and flag it.
	store.shards[rel] = []byte(eventLine(t, scopedEvent("e1", "2026-07-11T01:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)) + "\n")
	res2, err := r.ReadSince(context.Background(), res.Next)
	require.NoError(t, err)
	require.Len(t, res2.Events, 1, "shrunk shard re-read from the start")
	require.Len(t, res2.Report.Skipped, 1)
	assert.Contains(t, res2.Report.Skipped[0].Reason, "append-only violated")
}

func TestReadSincePartialTrailingLine(t *testing.T) {
	store := newMemStore()
	rel := shardRel("2026-07", "wA")
	full := eventLine(t, scopedEvent("e1", "2026-07-11T01:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand))
	// A complete line followed by a partial (mid-append) line with no newline.
	store.shards[rel] = []byte(full + "\n" + `{"v":1,"id":"partial`)
	r := readerOn(store)
	res, err := r.ReadSince(context.Background(), Cursor{})
	require.NoError(t, err)
	require.Len(t, res.Events, 1, "only the complete line is consumed")
	assert.Equal(t, int64(len(full)+1), res.Next.Offsets[rel], "offset stops at the last newline; partial line awaits next read")
}

func TestQueryAndReadSinceContextCancellation(t *testing.T) {
	store := newMemStore()
	store.put("2026-07", "wA", eventLine(t, scopedEvent("e1", "2026-07-11T01:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)))
	r := readerOn(store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := r.Query(ctx, Filter{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = r.ReadSince(ctx, Cursor{})
	require.ErrorIs(t, err, context.Canceled)
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
