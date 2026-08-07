package triage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The acceptance bar for the evidence schema is not that it validates neat
// fixtures, it is that it holds what the 2026-08-03 field trial actually
// produced. These records are adapted from
// workflow/triage-workitems/runs/design-2026-08-03/reviews/02-fest-lifecycle-and-workitems.md
// with the prose sections mapped onto the schema's fields.
//
// Adapting them is what surfaced the one real mismatch: the trial reported
// qualified confidence ("high for implementation state; medium-high for the
// refined routing diagnosis") on 12 of its 20 records, and "medium-high" is not
// in spec doc 04's high|medium|low vocabulary at all. The enum stayed as the
// spec defines it, because it is what makes confidence sortable, and the
// qualification moved to confidence_notes rather than being thrown away.

func fieldTrialRecords() []string {
	return []string{
		"fieldtrial_ritual_creation_lifecycle.json",
		"fieldtrial_festival_lifecycle_integration.json",
	}
}

func readFieldTrial(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return raw
}

// TestFieldTrialRecordsValidate is the acceptance bar: real trial output must
// pass strict parsing without being trimmed to fit.
func TestFieldTrialRecordsValidate(t *testing.T) {
	for _, name := range fieldTrialRecords() {
		t.Run(name, func(t *testing.T) {
			var record EvidenceRecord

			err := ParseDocument(readFieldTrial(t, name), &record, Strict)

			require.NoError(t, err)
			assert.NotEmpty(t, record.StableID)
			assert.NotEmpty(t, record.OriginalGoal)
			assert.NotEmpty(t, record.Delivered)
			assert.NotEmpty(t, record.Missing)
			assert.NotEmpty(t, record.Anchors)
			assert.Equal(t, EvidenceRoleEvidence, record.ProducedBy.Role)
		})
	}
}

// TestFieldTrialRecordsRoundTripByteStable proves the schema stores real
// content without reshaping it.
func TestFieldTrialRecordsRoundTripByteStable(t *testing.T) {
	for _, name := range fieldTrialRecords() {
		t.Run(name, func(t *testing.T) {
			var record EvidenceRecord
			require.NoError(t, ParseDocument(readFieldTrial(t, name), &record, Strict))

			first, err := MarshalDocument(&record)
			require.NoError(t, err)

			var again EvidenceRecord
			require.NoError(t, ParseDocument(first, &again, Strict))
			second, err := MarshalDocument(&again)
			require.NoError(t, err)

			assert.Equal(t, string(first), string(second))
		})
	}
}

// TestFieldTrialQualifiedConfidenceSurvives is the finding this fixture set
// exists to pin. The trial's compound confidence must reach a reader intact:
// the enum carries the overall level, the notes carry what it applies to.
func TestFieldTrialQualifiedConfidenceSurvives(t *testing.T) {
	var record EvidenceRecord
	require.NoError(t, ParseDocument(
		readFieldTrial(t, "fieldtrial_ritual_creation_lifecycle.json"), &record, Strict))

	assert.Equal(t, ConfidenceHigh, record.Confidence,
		"the sortable level stays in the spec's vocabulary")
	assert.Contains(t, record.ConfidenceNotes, "medium-high",
		"and the qualification the trial actually wrote is not discarded")
	assert.Contains(t, record.ConfidenceNotes, "routing diagnosis")
}

// TestFieldTrialRecordHoldsRealVolume: the trial's records are long. A schema
// that technically validates but truncates the content would pass the tests
// above and still be useless, so this pins that the substance survives.
func TestFieldTrialRecordHoldsRealVolume(t *testing.T) {
	var record EvidenceRecord
	require.NoError(t, ParseDocument(
		readFieldTrial(t, "fieldtrial_ritual_creation_lifecycle.json"), &record, Strict))

	assert.GreaterOrEqual(t, len(record.Missing), 3)
	assert.GreaterOrEqual(t, len(record.StaleAssumptions), 3)
	assert.GreaterOrEqual(t, len(record.OpenDecisions), 5)
	assert.Contains(t, strings.Join(record.StaleAssumptions, " "), "372 replacement markers",
		"specific findings must survive, not just their count")
}

// --- mutations, each naming its field ----------------------------------

// TestMutatedFieldTrialRecordsAreRejected is the other half of the Done When:
// a mutated copy must fail with every violated field named, not just the first.
func TestMutatedFieldTrialRecordsAreRejected(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(map[string]any)
		wantFields []string
	}{
		{
			name:       "confidence outside the vocabulary",
			mutate:     func(m map[string]any) { m["confidence"] = "medium-high" },
			wantFields: []string{"confidence"},
		},
		{
			name:       "no producer role",
			mutate:     func(m map[string]any) { delete(m["produced_by"].(map[string]any), "role") },
			wantFields: []string{"produced_by.role"},
		},
		{
			name:       "no original goal",
			mutate:     func(m map[string]any) { m["original_goal"] = "" },
			wantFields: []string{"original_goal"},
		},
		{
			name: "anchor kind that does not exist",
			mutate: func(m map[string]any) {
				m["anchors"].([]any)[0].(map[string]any)["kind"] = "commit"
			},
			wantFields: []string{"anchors[0].kind"},
		},
		{
			name: "path anchor with no algorithm on its hash",
			mutate: func(m map[string]any) {
				m["anchors"].([]any)[0].(map[string]any)["hash"] = "deadbeef"
			},
			wantFields: []string{"anchors[0].hash"},
		},
		{
			name:       "a typo in a field name",
			mutate:     func(m map[string]any) { m["stale_assumption"] = []any{"typo"} },
			wantFields: []string{"stale_assumption"},
		},
		{
			name: "several problems at once",
			mutate: func(m map[string]any) {
				m["confidence"] = "certain"
				m["stable_id"] = ""
				m["anchors"].([]any)[1].(map[string]any)["observed_stage"] = ""
			},
			wantFields: []string{"confidence", "stable_id", "anchors[1].observed_stage"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var doc map[string]any
			require.NoError(t, json.Unmarshal(
				readFieldTrial(t, "fieldtrial_ritual_creation_lifecycle.json"), &doc))
			tc.mutate(doc)
			mutated, err := json.Marshal(doc)
			require.NoError(t, err)

			parseErr := ParseDocument(mutated, &EvidenceRecord{}, Strict)

			require.Error(t, parseErr)
			require.ErrorIs(t, parseErr, camperrors.ErrInvalidInput)
			fields := violatedFields(parseErr)
			for _, want := range tc.wantFields {
				assert.Contains(t, fields, want, "error was: %v", parseErr)
			}
		})
	}
}

// TestMutatedRecordReportsEveryFieldAtOnce is the contract that makes a driver
// safe: three problems produce one list of three, not three round trips.
func TestMutatedRecordReportsEveryFieldAtOnce(t *testing.T) {
	var doc map[string]any
	require.NoError(t, json.Unmarshal(
		readFieldTrial(t, "fieldtrial_festival_lifecycle_integration.json"), &doc))
	doc["confidence"] = "certain"
	doc["stable_id"] = ""
	delete(doc["produced_by"].(map[string]any), "runtime")

	err := ParseDocument(mustMarshal(t, doc), &EvidenceRecord{}, Strict)

	require.Error(t, err)
	assert.ElementsMatch(t,
		[]string{"stable_id", "produced_by.runtime", "confidence"},
		violatedFields(err))
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}
