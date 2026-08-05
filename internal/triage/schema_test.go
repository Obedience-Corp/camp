package triage

import (
	"encoding/json"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- error cases first -------------------------------------------------

// TestParseDocumentRejectsForeignSchemaVersion covers the read-side version
// gate: a document from another format version is refused rather than decoded
// under this version's rules.
func TestParseDocumentRejectsForeignSchemaVersion(t *testing.T) {
	for _, tc := range allValidDocuments() {
		t.Run(tc.name, func(t *testing.T) {
			raw := marshalFor(t, tc.doc, tc.jsonl)
			swapped := strings.Replace(string(raw), SchemaVersion, "triage/v9", 1)

			err := ParseDocument([]byte(swapped), tc.empty(), Strict)

			require.Error(t, err)
			require.ErrorIs(t, err, camperrors.ErrInvalidInput)
			assert.Equal(t, []string{"schema_version"}, violatedFields(err))
			assert.Contains(t, err.Error(), "triage/v9")
			assert.Contains(t, err.Error(), SchemaVersion)
		})
	}
}

// TestParseDocumentRejectsUnknownFields covers the write-side strictness: an
// agent-supplied record with a typo is refused, and the report names the typo
// at its real path, including inside nested objects and slices.
func TestParseDocumentRejectsUnknownFields(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		target     func() Document
		wantFields []string
	}{
		{
			name:       "top level typo",
			raw:        withField(t, validEvidence(), false, `"confidance":"high"`),
			target:     func() Document { return &EvidenceRecord{} },
			wantFields: []string{"confidance"},
		},
		{
			name:       "nested object typo",
			raw:        replaceIn(t, validEvidence(), false, `"runtime":`, `"runtim":`),
			target:     func() Document { return &EvidenceRecord{} },
			wantFields: []string{"produced_by.runtim"},
		},
		{
			name:       "typo inside a slice element names its index",
			raw:        replaceIn(t, validEvidence(), false, `"repo":`, `"repository":`),
			target:     func() Document { return &EvidenceRecord{} },
			wantFields: []string{"anchors[0].repository"},
		},
		{
			name:       "typo inside a manifest row",
			raw:        replaceIn(t, validManifest(), false, `"batch":`, `"bach":`),
			target:     func() Document { return &Manifest{} },
			wantFields: []string{"rows[0].bach"},
		},
		{
			name:       "typo inside the embedded profile",
			raw:        replaceIn(t, validManifest(), false, `"batch_size":`, `"batchsize":`),
			target:     func() Document { return &Manifest{} },
			wantFields: []string{"profile.resolved.review.batchsize"},
		},
		{
			name:       "typo in a decision event line",
			raw:        withField(t, validDecision(), true, `"reason":"because"`),
			target:     func() Document { return &DecisionEvent{} },
			wantFields: []string{"reason"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ParseDocument([]byte(tc.raw), tc.target(), Strict)

			require.Error(t, err)
			require.ErrorIs(t, err, camperrors.ErrInvalidInput)
			for _, want := range tc.wantFields {
				assert.Contains(t, violatedFields(err), want)
			}
		})
	}
}

// TestParseDocumentLenientToleratesAdditiveFields is the other half of the
// asymmetry: the store opens a run written by a newer camp instead of
// refusing it.
func TestParseDocumentLenientToleratesAdditiveFields(t *testing.T) {
	raw := withField(t, validEvidence(), false, `"future_field":"from a newer camp"`)

	strictErr := ParseDocument([]byte(raw), &EvidenceRecord{}, Strict)
	require.Error(t, strictErr)

	var record EvidenceRecord
	require.NoError(t, ParseDocument([]byte(raw), &record, Lenient))
	assert.Equal(t, "design-festival-hub-control-plane-2026-08-04", record.StableID)
}

// TestValidateReportsEveryViolation is the contract spec doc 03 states as
// "rejection lists every violated field": one submission, one complete list.
func TestValidateReportsEveryViolation(t *testing.T) {
	record := validEvidence()
	record.StableID = ""
	record.Confidence = "certain"
	record.ProducedBy.Role = "oracle"
	record.ProducedBy.Runtime = ""
	record.Anchors[0].Repo = ""

	violations := record.Validate()
	fields := fieldsOf(violations)

	assert.ElementsMatch(t, []string{
		"stable_id",
		"confidence",
		"produced_by.role",
		"produced_by.runtime",
		"anchors[0].repo",
	}, fields)
}

// TestValidationNamesAllowedValues checks a rejected enum tells the user what
// it would have accepted.
func TestValidationNamesAllowedValues(t *testing.T) {
	record := validEvidence()
	record.Confidence = "certain"

	violations := record.Validate()

	require.Len(t, violations, 1)
	assert.Equal(t, "confidence", violations[0].Field)
	assert.Equal(t, Confidences(), violations[0].Allowed)
	assert.Contains(t, violations[0].String(), "allowed: high, medium, low")
}

// TestMarshalDocumentRefusesInvalidDocuments proves an invalid document cannot
// reach disk through the package's own encoder.
func TestMarshalDocumentRefusesInvalidDocuments(t *testing.T) {
	manifest := validManifest()
	manifest.Rows[0].Batch = 0

	_, err := MarshalDocument(manifest)

	require.Error(t, err)
	require.ErrorIs(t, err, camperrors.ErrInvalidInput)
	assert.Contains(t, violatedFields(err), "rows[0].batch")
}

// TestManifestRejectsDuplicateRows guards the join key every other run file
// depends on.
func TestManifestRejectsDuplicateRows(t *testing.T) {
	manifest := validManifest()
	manifest.Rows[1].StableID = manifest.Rows[0].StableID

	assert.Contains(t, fieldsOf(manifest.Validate()), "rows[1].stable_id")
}

// TestManifestRequiresBaseRunForIncremental: an incremental run with no base
// has nothing to carry from, so the mode is a lie.
func TestManifestRequiresBaseRunForIncremental(t *testing.T) {
	manifest := validManifest()
	manifest.BaseRunID = nil

	assert.Contains(t, fieldsOf(manifest.Validate()), "base_run_id")

	manifest.Mode = RunModeFull
	assert.Empty(t, manifest.Validate())
}

// TestEvidenceNoEvidenceRejectsJudgment: the no-evidence marker records that
// no reading happened, so carrying findings contradicts itself.
func TestEvidenceNoEvidenceRejectsJudgment(t *testing.T) {
	record := validEvidence()
	record.NoEvidence = true

	fields := fieldsOf(record.Validate())

	assert.ElementsMatch(t, []string{
		"original_goal", "confidence", "delivered", "missing",
		"stale_assumptions", "open_decisions",
	}, fields)
}

// --- round trips -------------------------------------------------------

// TestDocumentsRoundTripByteStable is the determinism guarantee: rendering a
// run's files from the same data twice produces the same bytes.
func TestDocumentsRoundTripByteStable(t *testing.T) {
	for _, tc := range allValidDocuments() {
		t.Run(tc.name, func(t *testing.T) {
			first := marshalFor(t, tc.doc, tc.jsonl)

			decoded := tc.empty()
			require.NoError(t, ParseDocument(first, decoded, Strict))

			second := marshalFor(t, decoded, tc.jsonl)
			assert.Equal(t, string(first), string(second))
		})
	}
}

// TestNormalizeIsIdempotent: normalizing an already-canonical document must
// not move it, or repeated writes would churn committed files.
func TestNormalizeIsIdempotent(t *testing.T) {
	for _, tc := range allValidDocuments() {
		t.Run(tc.name, func(t *testing.T) {
			first := marshalFor(t, tc.doc, tc.jsonl)
			second := marshalFor(t, tc.doc, tc.jsonl)
			assert.Equal(t, string(first), string(second))
		})
	}
}

// TestJSONLRecordsAreSingleLines: the append-only streams must stay one
// record per line or the fold cannot read them back.
func TestJSONLRecordsAreSingleLines(t *testing.T) {
	for _, tc := range allValidDocuments() {
		if !tc.jsonl {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			line, err := MarshalLine(tc.doc)
			require.NoError(t, err)
			assert.Equal(t, 1, strings.Count(string(line), "\n"))
			assert.True(t, strings.HasSuffix(string(line), "\n"))
		})
	}
}

// TestManifestFieldNamesMatchSpec pins the wire names of the manifest against
// spec doc 04. A rename here breaks every consumer, so it must be deliberate.
func TestManifestFieldNamesMatchSpec(t *testing.T) {
	raw, err := MarshalDocument(validManifest())
	require.NoError(t, err)

	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.ElementsMatch(t, []string{
		"schema_version", "run_id", "mode", "profile", "base_run_id",
		"created_at", "rows",
	}, keysOf(doc))

	var rows []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(doc["rows"], &rows))
	assert.ElementsMatch(t, []string{
		"stable_id", "ref", "key", "type", "title", "relative_path",
		"lifecycle_stage", "attention_stage", "batch", "policy",
		"carried_from", "identity_exception",
	}, keysOf(rows[0]))
}

// TestEvidenceFieldNamesMatchSpec pins the evidence contract agents write
// against.
func TestEvidenceFieldNamesMatchSpec(t *testing.T) {
	raw, err := MarshalDocument(validEvidence())
	require.NoError(t, err)

	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.ElementsMatch(t, []string{
		"schema_version", "stable_id", "original_goal", "delivered", "missing",
		"stale_assumptions", "related", "open_decisions", "confidence",
		"anchors", "produced_by",
	}, keysOf(doc))
}

// TestNilSlicesSerializeAsEmptyArrays keeps triage consistent with camp's
// existing JSON convention: absent lists are [], never null.
func TestNilSlicesSerializeAsEmptyArrays(t *testing.T) {
	record := &EvidenceRecord{
		StableID:     "x",
		OriginalGoal: "goal",
		Confidence:   ConfidenceLow,
		ProducedBy:   ProducedBy{Role: EvidenceRoleHuman, Runtime: "human", At: testAt},
	}

	raw, err := MarshalDocument(record)
	require.NoError(t, err)

	assert.NotContains(t, string(raw), "null")
	assert.Contains(t, string(raw), `"delivered": []`)
	assert.Contains(t, string(raw), `"anchors": []`)
}

// --- helpers -----------------------------------------------------------

func marshalFor(t *testing.T, doc Document, jsonl bool) []byte {
	t.Helper()
	var (
		raw []byte
		err error
	)
	if jsonl {
		raw, err = MarshalLine(doc)
	} else {
		raw, err = MarshalDocument(doc)
	}
	require.NoError(t, err)
	return raw
}

// withField re-encodes doc with one extra raw JSON member spliced in.
func withField(t *testing.T, doc Document, jsonl bool, member string) string {
	t.Helper()
	raw := strings.TrimSpace(string(marshalFor(t, doc, jsonl)))
	require.True(t, strings.HasPrefix(raw, "{"))
	return "{" + member + "," + raw[1:]
}

// replaceIn re-encodes doc and rewrites the first occurrence of old.
func replaceIn(t *testing.T, doc Document, jsonl bool, old, replacement string) string {
	t.Helper()
	raw := string(marshalFor(t, doc, jsonl))
	require.Contains(t, raw, old)
	return strings.Replace(raw, old, replacement, 1)
}

func violatedFields(err error) []string {
	return fieldsOf(Violations(err))
}

func fieldsOf(violations []Violation) []string {
	out := make([]string, 0, len(violations))
	for _, v := range violations {
		out = append(out, v.Field)
	}
	return out
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
