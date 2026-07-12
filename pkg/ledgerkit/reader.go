package ledgerkit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sort"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// SkippedLine records a ledger line the reader could not use, so the doctor can
// surface malformed or degraded data instead of silently dropping it.
type SkippedLine struct {
	Shard  string // shard path relative to the campaign root, "" for in-memory
	Line   int    // 1-based line number within the shard
	Reason string
}

// ReadReport carries non-fatal diagnostics from a read: lines skipped and the
// count of unknown-version envelopes seen (kept, but flagged for the doctor).
type ReadReport struct {
	Skipped         []SkippedLine
	UnknownVersions int
}

// Reader reads and merges every shard under a campaign's ledger. Filesystem
// access is via an injected shardStore so reads are unit-testable in memory.
type Reader struct {
	store shardStore
}

// NewReader returns a Reader for the campaign at campaignRoot.
func NewReader(campaignRoot string) (*Reader, error) {
	if campaignRoot == "" {
		return nil, camperrors.New("ledgerkit: empty campaign root")
	}
	return &Reader{store: osShardStore{campaignRoot: campaignRoot}}, nil
}

// Read returns every event across all shards, ordered by ts then id and
// de-duplicated by id, plus a report of anything skipped. A missing events
// directory is not an error: a campaign with no ledger yet reads as empty.
func (r *Reader) Read(ctx context.Context) ([]*Event, *ReadReport, error) {
	return r.Query(ctx, Filter{})
}

// ParseShard parses one shard's lines into events, collecting malformed or
// incomplete lines into skipped rather than failing the whole read. It is pure
// (reads from r, does no filesystem access) so it is unit-testable. shard labels
// the SkippedLine entries. Unknown top-level fields are ignored by
// encoding/json; unknown kinds are kept.
func ParseShard(shard string, r io.Reader) ([]*Event, []SkippedLine, int) {
	var events []*Event
	var skipped []SkippedLine
	unknownVersions := 0
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			skipped = append(skipped, SkippedLine{Shard: shard, Line: lineNo, Reason: "invalid json: " + err.Error()})
			continue
		}
		if ev.ID == "" {
			skipped = append(skipped, SkippedLine{Shard: shard, Line: lineNo, Reason: "missing id"})
			continue
		}
		if ev.TS == "" {
			skipped = append(skipped, SkippedLine{Shard: shard, Line: lineNo, Reason: "missing ts"})
			continue
		}
		if ev.V != EnvelopeVersion {
			unknownVersions++ // kept: additive tolerance (D001), flagged for doctor
		}
		events = append(events, &ev)
	}
	if err := sc.Err(); err != nil {
		skipped = append(skipped, SkippedLine{Shard: shard, Line: lineNo, Reason: "read error: " + err.Error()})
	}
	return events, skipped, unknownVersions
}

// Merge combines events from many shards into one stream ordered by timestamp
// then id, de-duplicated by id (id-keyed dedupe on read, D002). It is pure and
// checks ctx so large reads stay cancelable. parseTS failures sort last but are
// retained (coarse/degraded timestamps tolerated, D001 finalization).
func Merge(ctx context.Context, shards [][]*Event) ([]*Event, error) {
	seen := make(map[string]struct{})
	var out []*Event
	for _, shard := range shards {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, ev := range shard {
			if _, dup := seen[ev.ID]; dup {
				continue
			}
			seen[ev.ID] = struct{}{}
			out = append(out, ev)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := parseTS(out[i].TS), parseTS(out[j].TS)
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// parseTS parses a ledger timestamp, returning the zero time (which sorts
// first) only for empty input; unparseable non-empty timestamps sort to the far
// future so they trail valid events without being dropped.
func parseTS(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	return time.Unix(1<<62, 0)
}
