package ledgerkit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnoseMalformedAndUnknownVersion(t *testing.T) {
	store := newMemStore()
	rel := shardRel("2026-07", "wA")
	store.shards[rel] = []byte(
		eventLine(t, scopedEvent("ok", "2026-07-11T01:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)) + "\n" +
			`{not json}` + "\n" +
			`{"v":99,"id":"newer","ts":"2026-07-11T02:00:00Z","kind":"created","actor":{"type":"human"},"source":"command"}` + "\n",
	)
	diag, err := readerOn(store).Diagnose(context.Background())
	require.NoError(t, err)
	require.Len(t, diag.Skipped, 1, "the malformed line is reported")
	assert.Contains(t, diag.Skipped[0].Reason, "invalid json")
	assert.Equal(t, 1, diag.UnknownVersions, "the newer-version event is counted, not dropped")
	assert.Equal(t, 2, diag.EventCount, "valid + newer-version events are both kept")
}

func TestDiagnoseDuplicateIDsAcrossShards(t *testing.T) {
	store := newMemStore()
	store.put("2026-07", "wA", eventLine(t, scopedEvent("dup", "2026-07-11T01:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)))
	store.put("2026-07", "wB", eventLine(t, scopedEvent("dup", "2026-07-11T02:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)))
	diag, err := readerOn(store).Diagnose(context.Background())
	require.NoError(t, err)
	require.Len(t, diag.Duplicates, 1)
	assert.Equal(t, "dup", diag.Duplicates[0].ID)
	assert.Len(t, diag.Duplicates[0].Locations, 2, "both occurrences are located")
	assert.Equal(t, 1, diag.EventCount, "the merged stream still dedupes to one event")
}

func TestDiagnoseShardNamingViolation(t *testing.T) {
	store := newMemStore()
	// A shard under a month directory that is not a valid YYYY-MM.
	store.put("backup", "wA", eventLine(t, scopedEvent("e1", "2026-07-11T01:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)))
	diag, err := readerOn(store).Diagnose(context.Background())
	require.NoError(t, err)
	require.Len(t, diag.ShardViolations, 1)
	assert.Contains(t, diag.ShardViolations[0].Reason, "not a valid YYYY-MM")
}

func TestDiagnoseClean(t *testing.T) {
	store := newMemStore()
	store.put("2026-07", "wA", eventLine(t, scopedEvent("e1", "2026-07-11T01:00:00Z", KindCreated, Scope{Campaign: "c1"}, SourceCommand)))
	diag, err := readerOn(store).Diagnose(context.Background())
	require.NoError(t, err)
	assert.Empty(t, diag.Skipped)
	assert.Empty(t, diag.Duplicates)
	assert.Empty(t, diag.ShardViolations)
	assert.Zero(t, diag.UnknownVersions)
	assert.Equal(t, 1, diag.EventCount)
}
