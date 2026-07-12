package ledgerkit

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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

// ParseShard parses one shard's lines into events, collecting malformed or
// incomplete lines into skipped rather than failing the whole read. It is pure
// (reads from r, does no disk access) so it is unit-testable. shard labels the
// SkippedLine entries. Unknown top-level fields are ignored by encoding/json;
// unknown kinds are kept.
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
		if len(trimSpace(raw)) == 0 {
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

// Reader reads and merges every shard under a campaign's ledger.
type Reader struct {
	campaignRoot string
}

// NewReader returns a Reader for the campaign at campaignRoot.
func NewReader(campaignRoot string) (*Reader, error) {
	if campaignRoot == "" {
		return nil, camperrors.New("ledgerkit: empty campaign root")
	}
	return &Reader{campaignRoot: campaignRoot}, nil
}

// Read globs every shard, parses each tolerantly, and returns the merged event
// stream plus a report of anything skipped. A missing events directory is not
// an error: a campaign with no ledger yet reads as empty.
func (r *Reader) Read(ctx context.Context) ([]*Event, *ReadReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	root := filepath.Join(r.campaignRoot, EventsDir)
	pattern := filepath.Join(root, "*", "*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, nil, camperrors.Wrapf(err, "ledgerkit: glob shards under %s", root)
	}
	sort.Strings(matches)
	report := &ReadReport{}
	shards := make([][]*Event, 0, len(matches))
	for _, path := range matches {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		rel, relErr := filepath.Rel(r.campaignRoot, path)
		if relErr != nil {
			rel = path
		}
		events, skipped, unknown := parseShardFile(path, rel)
		report.Skipped = append(report.Skipped, skipped...)
		report.UnknownVersions += unknown
		shards = append(shards, events)
	}
	merged, err := Merge(ctx, shards)
	if err != nil {
		return nil, nil, err
	}
	return merged, report, nil
}

// parseShardFile opens and parses one shard file. An open failure is reported
// as a skipped line rather than aborting the whole read.
func parseShardFile(path, label string) ([]*Event, []SkippedLine, int) {
	f, err := os.Open(path)
	if err != nil {
		return nil, []SkippedLine{{Shard: label, Line: 0, Reason: "open failed: " + err.Error()}}, 0
	}
	defer func() { _ = f.Close() }()
	return ParseShard(label, f)
}

// trimSpace reports the byte slice with surrounding ASCII whitespace removed,
// used to detect blank lines without allocating a string.
func trimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isSpace(b[start]) {
		start++
	}
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
