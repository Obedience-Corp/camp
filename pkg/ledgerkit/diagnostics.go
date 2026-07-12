package ledgerkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Duplicate reports an event id that appears more than once across the ledger
// (within or across shards). Ids are unique by construction, so any duplicate
// is an integrity concern the doctor surfaces.
type Duplicate struct {
	ID        string
	Locations []string // "<shard rel>:<line>" for each occurrence
}

// ShardViolation reports a shard file whose location does not match the D002
// naming scheme (.campaign/events/<YYYY-MM>/<writer>.jsonl), e.g. a month
// directory that is not a valid YYYY-MM.
type ShardViolation struct {
	Shard  string
	Reason string
}

// Diagnostics is the ledger integrity picture the doctor consumes: it is a
// raw (non-deduplicated) pass so problems the normal read hides (duplicate
// ids) become visible, plus the merged event stream for downstream checks
// (evidence resolution).
type Diagnostics struct {
	EventCount      int
	Skipped         []SkippedLine    // malformed/truncated lines
	UnknownVersions int              // envelopes with a v this binary does not know
	Duplicates      []Duplicate      // ids seen more than once
	ShardViolations []ShardViolation // shard paths violating the naming scheme
	Events          []*Event         // merged, deduped, ordered
}

// Diagnose reads every shard raw (no dedupe) and returns integrity diagnostics.
// It is the input to camp doctor's ledger health check. Filesystem access is via
// the injected store, so it is unit-testable in memory.
func (r *Reader) Diagnose(ctx context.Context) (*Diagnostics, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	refs, err := r.store.list()
	if err != nil {
		return nil, err
	}
	diag := &Diagnostics{}
	idLocations := map[string][]string{}
	var shards [][]*Event
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, monthErr := time.Parse("2006-01", ref.Month); monthErr != nil {
			diag.ShardViolations = append(diag.ShardViolations, ShardViolation{
				Shard:  ref.Rel,
				Reason: fmt.Sprintf("month directory %q is not a valid YYYY-MM", ref.Month),
			})
		}
		data, err := r.store.readFrom(ref.Rel, 0)
		if err != nil {
			return nil, err
		}
		events, skipped, unknown := ParseShard(ref.Rel, bytes.NewReader(data))
		diag.Skipped = append(diag.Skipped, skipped...)
		diag.UnknownVersions += unknown
		recordLocations(idLocations, ref.Rel, data)
		shards = append(shards, events)
	}
	for id, locs := range idLocations {
		if len(locs) > 1 {
			diag.Duplicates = append(diag.Duplicates, Duplicate{ID: id, Locations: locs})
		}
	}
	sortDuplicates(diag.Duplicates)
	merged, err := Merge(ctx, shards)
	if err != nil {
		return nil, err
	}
	diag.Events = merged
	diag.EventCount = len(merged)
	return diag, nil
}

// recordLocations appends "<shard>:<line>" for every event id in the shard
// data, so duplicate ids can be reported with all their occurrences. It scans
// lines directly (not via the deduping reader) so repeats are visible.
func recordLocations(idLocations map[string][]string, shard string, data []byte) {
	lineNo := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		lineNo++
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var probe struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(line, &probe); err != nil || probe.ID == "" {
			continue
		}
		idLocations[probe.ID] = append(idLocations[probe.ID], fmt.Sprintf("%s:%d", shard, lineNo))
	}
}

// sortDuplicates orders duplicates by id for deterministic reporting.
func sortDuplicates(dups []Duplicate) {
	for i := 1; i < len(dups); i++ {
		for j := i; j > 0 && dups[j-1].ID > dups[j].ID; j-- {
			dups[j-1], dups[j] = dups[j], dups[j-1]
		}
	}
}
