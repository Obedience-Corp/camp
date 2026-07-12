package ledgerkit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// cursorVersion is the on-the-wire format version of an encoded cursor.
const cursorVersion = 1

// Cursor is an opaque incremental-read bookmark: per-shard byte offsets of how
// far each shard has already been consumed. Downstream ingesters (camp-graph
// refresh, festival-app-style tailers) persist the encoded token and pass it
// back to ReadSince so refresh cost is proportional to new events (D008).
//
// The zero Cursor (nil offsets) reads every shard from the beginning. Shards
// are keyed by their path relative to the campaign root, so the token is stable
// across machines and checkouts.
type Cursor struct {
	Version int              `json:"v"`
	Offsets map[string]int64 `json:"offsets"`
}

// Encode serializes the cursor to an opaque, transport-safe string.
func (c Cursor) Encode() (string, error) {
	if c.Version == 0 {
		c.Version = cursorVersion
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", camperrors.Wrap(err, "ledgerkit: encode cursor")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodeCursor parses a token produced by Encode. An empty string decodes to
// the zero cursor (read from the beginning), so first-time consumers pass "".
func DecodeCursor(token string) (Cursor, error) {
	if token == "" {
		return Cursor{Version: cursorVersion, Offsets: map[string]int64{}}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, camperrors.Wrap(err, "ledgerkit: decode cursor token")
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return Cursor{}, camperrors.Wrap(err, "ledgerkit: unmarshal cursor")
	}
	if c.Offsets == nil {
		c.Offsets = map[string]int64{}
	}
	return c, nil
}

// SinceResult is the outcome of an incremental read: the new events, the cursor
// to persist for the next call, and diagnostics (including any append-only
// violations that forced a shard re-read).
type SinceResult struct {
	Events []*Event
	Next   Cursor
	Report *ReadReport
}

// ReadSince returns events appended since the given cursor and the next cursor.
// It reads only the tail of each shard past its recorded offset, so cost is
// proportional to new data, and it handles ledger growth transparently:
//   - a new month shard or a new writer's shard is absent from the cursor and
//     is read from offset 0;
//   - a shard whose size is smaller than its recorded offset was rewritten or
//     truncated (an append-only violation the doctor also flags); its cursor is
//     invalidated and the whole shard is re-read from 0, deterministically, so
//     downstream state converges rather than silently missing events.
//
// Offsets always advance to a line boundary: a trailing partial line (a
// concurrent mid-append) is left unconsumed and picked up on the next call.
func (r *Reader) ReadSince(ctx context.Context, cur Cursor) (*SinceResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	refs, err := r.store.list()
	if err != nil {
		return nil, err
	}
	next := Cursor{Version: cursorVersion, Offsets: map[string]int64{}}
	report := &ReadReport{}
	var shards [][]*Event
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prev := cur.Offsets[ref.Rel]
		size, err := r.store.size(ref.Rel)
		if err != nil {
			return nil, err
		}
		start := prev
		if size < prev {
			// Append-only violation: history was rewritten. Re-read from the top
			// so downstream state deterministically converges instead of skipping.
			report.Skipped = append(report.Skipped, SkippedLine{
				Shard: ref.Rel, Line: 0,
				Reason: "shard shrank below cursor offset; append-only violated, re-reading from start",
			})
			start = 0
		}
		if size == start {
			next.Offsets[ref.Rel] = start
			continue // nothing new
		}
		data, err := r.store.readFrom(ref.Rel, start)
		if err != nil {
			return nil, err
		}
		events, end, skipped, unknown := consumeTail(ref.Rel, data, start)
		report.Skipped = append(report.Skipped, skipped...)
		report.UnknownVersions += unknown
		next.Offsets[ref.Rel] = end
		shards = append(shards, events)
	}
	merged, err := Merge(ctx, shards)
	if err != nil {
		return nil, err
	}
	return &SinceResult{Events: merged, Next: next, Report: report}, nil
}

// consumeTail parses shard bytes read starting at start, consuming only through
// the last complete line so a trailing partial line (a concurrent mid-append)
// is left for the next read. It returns the parsed events, the new consumed
// offset (a line boundary), skipped lines, and the unknown-version count. It is
// pure for testability.
func consumeTail(label string, data []byte, start int64) ([]*Event, int64, []SkippedLine, int) {
	lastNL := bytes.LastIndexByte(data, '\n')
	if lastNL < 0 {
		return nil, start, nil, 0 // no complete line yet
	}
	events, skipped, unknown := ParseShard(label, bytes.NewReader(data[:lastNL+1]))
	return events, start + int64(lastNL) + 1, skipped, unknown
}
