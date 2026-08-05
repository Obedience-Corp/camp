package triage

import (
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- apply plan, receipts, verification --------------------------------

// TestApplyPlanRejects covers the rules that keep a plan executable.
func TestApplyPlanRejects(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*ApplyPlan)
		wantFields []string
	}{
		{
			name:       "entry with nothing to run",
			mutate:     func(p *ApplyPlan) { p.Entries[0].Commands = nil },
			wantFields: []string{"entries[0].commands"},
		},
		{
			name:       "command with an empty argv",
			mutate:     func(p *ApplyPlan) { p.Entries[0].Commands[0].Argv = nil },
			wantFields: []string{"entries[0].commands[0].argv"},
		},
		{
			name:       "command with an unknown kind",
			mutate:     func(p *ApplyPlan) { p.Entries[0].Commands[1].Kind = "sudo" },
			wantFields: []string{"entries[0].commands[1].kind"},
		},
		{
			name: "successors precondition naming no successors",
			mutate: func(p *ApplyPlan) {
				p.Entries[0].Preconditions[0].IDs = nil
			},
			wantFields: []string{"entries[0].preconditions[0].ids"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := validPlan()
			tc.mutate(plan)

			fields := fieldsOf(plan.Validate())

			for _, want := range tc.wantFields {
				assert.Contains(t, fields, want)
			}
		})
	}
}

// TestReceiptRejects: a receipt is the record verification trusts, so a
// failure with no error text or an applied action with no end time is not a
// usable receipt.
func TestReceiptRejects(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*Receipt)
		wantFields []string
	}{
		{
			name:       "failed with no error text",
			mutate:     func(r *Receipt) { r.Result = ReceiptFailed; r.Error = "" },
			wantFields: []string{"error"},
		},
		{
			name:       "applied with no finish time",
			mutate:     func(r *Receipt) { r.FinishedAt = time.Time{} },
			wantFields: []string{"finished_at"},
		},
		{
			name:       "finished before it started",
			mutate:     func(r *Receipt) { r.FinishedAt = r.StartedAt.Add(-time.Minute) },
			wantFields: []string{"finished_at"},
		},
		{
			name:       "no argv",
			mutate:     func(r *Receipt) { r.Argv = nil },
			wantFields: []string{"argv"},
		},
		{
			name:       "unknown result",
			mutate:     func(r *Receipt) { r.Result = "maybe" },
			wantFields: []string{"result"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receipt := validReceipt()
			tc.mutate(receipt)

			fields := fieldsOf(receipt.Validate())

			for _, want := range tc.wantFields {
				assert.Contains(t, fields, want)
			}
		})
	}
}

// TestSkippedReceiptNeedsNoOutcome: a skipped command never ran, so it has no
// finish time and no error.
func TestSkippedReceiptNeedsNoOutcome(t *testing.T) {
	receipt := validReceipt()
	receipt.Result = ReceiptSkipped
	receipt.FinishedAt = time.Time{}
	receipt.Commit = ""

	assert.Empty(t, receipt.Validate())
}

// TestVerificationTotalsMustMatchRows stops a report from claiming a clean run
// over rows that say otherwise.
func TestVerificationTotalsMustMatchRows(t *testing.T) {
	report := validVerification()
	report.Totals.Mismatched = 0

	assert.Contains(t, fieldsOf(report.Validate()), "totals")
}

// TestVerificationNormalizeRecomputesTotals: writers hand over rows, and the
// tally is derived rather than trusted.
func TestVerificationNormalizeRecomputesTotals(t *testing.T) {
	report := validVerification()
	report.Totals = VerificationTotals{Checked: 99, Matched: 99}

	report.Normalize()

	assert.Equal(t, VerificationTotals{Checked: 2, Matched: 1, Mismatched: 1}, report.Totals)
	assert.Empty(t, report.Validate())
}

// --- profile -----------------------------------------------------------

// TestProfileRejects covers the profile rules a run validates before it
// snapshots anything.
func TestProfileRejects(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*ResolvedProfile)
		wantFields []string
	}{
		{
			name:       "foreign profile schema version",
			mutate:     func(p *ResolvedProfile) { p.SchemaVersion = "triage-profile/v2" },
			wantFields: []string{"profile.resolved.schema_version"},
		},
		{
			name:       "unknown group_by",
			mutate:     func(p *ResolvedProfile) { p.Review.GroupBy = "vibes" },
			wantFields: []string{"profile.resolved.review.group_by"},
		},
		{
			name:       "batch size of zero",
			mutate:     func(p *ResolvedProfile) { p.Review.BatchSize = 0 },
			wantFields: []string{"profile.resolved.review.batch_size"},
		},
		{
			name:       "unknown routing tier",
			mutate:     func(p *ResolvedProfile) { p.Routing.EvidenceTier = "genius" },
			wantFields: []string{"profile.resolved.routing.evidence_tier"},
		},
		{
			name:       "no concurrency at all",
			mutate:     func(p *ResolvedProfile) { p.Routing.MaxConcurrent = 0 },
			wantFields: []string{"profile.resolved.routing.max_concurrent"},
		},
		{
			name: "depth for a stage that does not exist",
			mutate: func(p *ResolvedProfile) {
				p.Evidence.DepthByStage["someday"] = EvidenceDepthNone
			},
			wantFields: []string{"profile.resolved.evidence.depth_by_stage.someday"},
		},
		{
			name: "lane with no depth",
			mutate: func(p *ResolvedProfile) {
				delete(p.Evidence.DepthByStage, "parked")
			},
			wantFields: []string{"profile.resolved.evidence.depth_by_stage.parked"},
		},
		{
			name: "unknown depth value",
			mutate: func(p *ResolvedProfile) {
				p.Evidence.DepthByStage["parked"] = "skim"
			},
			wantFields: []string{"profile.resolved.evidence.depth_by_stage.parked"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := validManifest()
			tc.mutate(&manifest.Profile.Resolved)

			fields := fieldsOf(manifest.Validate())

			for _, want := range tc.wantFields {
				assert.Contains(t, fields, want)
			}
		})
	}
}

// TestDefaultProfileIsValid: the built-in every campaign starts from must pass
// the validation it will be judged by.
func TestDefaultProfileIsValid(t *testing.T) {
	profile := DefaultProfile()

	assert.Empty(t, profile.validate("profile"))
	assert.Equal(t, ProfileSchemaVersion, profile.SchemaVersion)
	assert.Len(t, profile.Evidence.DepthByStage, len(evidenceStageKeys()))
}

// --- manifest rows -----------------------------------------------------

// TestManifestRowRejects covers the identity rules that make a row safe to
// mutate later.
func TestManifestRowRejects(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*ManifestRow)
		wantFields []string
	}{
		{
			name:       "no stable id",
			mutate:     func(r *ManifestRow) { r.StableID = "" },
			wantFields: []string{"rows[0].stable_id"},
		},
		{
			name:       "malformed ref",
			mutate:     func(r *ManifestRow) { r.Ref = "fa0c2a" },
			wantFields: []string{"rows[0].ref"},
		},
		{
			name:       "unknown lifecycle stage",
			mutate:     func(r *ManifestRow) { r.LifecycleStage = "limbo" },
			wantFields: []string{"rows[0].lifecycle_stage"},
		},
		{
			name:       "unknown attention stage",
			mutate:     func(r *ManifestRow) { r.AttentionStage = "urgent" },
			wantFields: []string{"rows[0].attention_stage"},
		},
		{
			name:       "unbatched row",
			mutate:     func(r *ManifestRow) { r.Batch = 0 },
			wantFields: []string{"rows[0].batch"},
		},
		{
			name:       "unknown evidence depth",
			mutate:     func(r *ManifestRow) { r.Policy.Evidence = "skim" },
			wantFields: []string{"rows[0].policy.evidence"},
		},
		{
			name: "identity exception with no path to bind to",
			mutate: func(r *ManifestRow) {
				r.IdentityException = &IdentityException{Reason: "legacy", GrantedBy: "x", GrantedAt: testAt}
			},
			wantFields: []string{"rows[0].identity_exception.path"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := validManifest()
			tc.mutate(&manifest.Rows[0])

			fields := fieldsOf(manifest.Validate())

			for _, want := range tc.wantFields {
				assert.Contains(t, fields, want)
			}
		})
	}
}

// TestManifestRowAcceptsLegacyIdentity: an item with no ref still triages when
// it carries the path-bound exception FT-008 called for.
func TestManifestRowAcceptsLegacyIdentity(t *testing.T) {
	manifest := validManifest()

	require.Empty(t, manifest.Rows[1].Ref)
	require.NotNil(t, manifest.Rows[1].IdentityException)
	assert.Empty(t, manifest.Validate())
}

// --- error surface -----------------------------------------------------

// TestValidationErrorMatchesInvalidInput lets CLI surfaces map any schema
// rejection to exit code 1 without type-asserting.
func TestValidationErrorMatchesInvalidInput(t *testing.T) {
	err := newValidationError("evidence record", []Violation{
		{Field: "confidence", Message: "unknown value", Allowed: Confidences()},
	})

	require.Error(t, err)
	assert.True(t, camperrors.Is(err, camperrors.ErrInvalidInput))
	assert.Contains(t, err.Error(), "invalid evidence record: 1 problem")
	assert.Contains(t, err.Error(), "confidence: unknown value (allowed: high, medium, low)")
}

// TestNewValidationErrorIsNilWhenClean avoids the typed-nil trap: a clean
// document must produce an error value that compares equal to nil.
func TestNewValidationErrorIsNilWhenClean(t *testing.T) {
	assert.NoError(t, newValidationError("run manifest", nil))
}

// TestViolationsIgnoresOtherErrors keeps callers from mistaking an IO failure
// for a schema problem.
func TestViolationsIgnoresOtherErrors(t *testing.T) {
	assert.Nil(t, Violations(camperrors.New("disk on fire")))
}
