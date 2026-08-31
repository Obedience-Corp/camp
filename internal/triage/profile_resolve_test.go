package triage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obedience-Corp/camp/internal/triage/scaffold"
)

// scaffolded returns a campaign root with the shipped scaffold in place.
func scaffolded(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	_, err := scaffold.Ensure(context.Background(), root)
	require.NoError(t, err)
	return root
}

// writeProfile replaces the campaign profile.
func writeProfile(t *testing.T, root, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, scaffold.DirName, "profile.yaml"), []byte(body), 0o644))
}

// TestAcceptance4_ResolvedProfileIsPrintable is CONFIGURABILITY acceptance 4:
// the merged result is inspectable before anything runs.
func TestAcceptance4_ResolvedProfileIsPrintable(t *testing.T) {
	root := scaffolded(t)

	resolution, err := ResolveProfileNamed(context.Background(), root, "")
	require.NoError(t, err)

	assert.True(t, resolution.FromFile, "the campaign's own profile is used when present")
	assert.Equal(t, ProfileNameDefault, resolution.Name)
	assert.Equal(t, RunModeIncremental, resolution.Profile.Runs.Mode)
	assert.Equal(t, 5, resolution.Profile.Review.BatchSize)
	assert.Equal(t, 5, resolution.Profile.Anchors.RecheckMinutes,
		"the scaffolded anchors block resolves, not just the ones that predate it")
	assert.Len(t, resolution.TypePolicies, 5)
}

// TestAcceptance12_UnknownTypeTriagesWithZeroConfiguration: a type nobody
// configured still has a full vocabulary, from types/_default.yaml.
func TestAcceptance12_UnknownTypeTriagesWithZeroConfiguration(t *testing.T) {
	root := scaffolded(t)

	resolution, err := ResolveProfileNamed(context.Background(), root, "")
	require.NoError(t, err)

	fallback, ok := resolution.TypePolicies["_default"]
	require.True(t, ok, "the fallback policy ships")
	assert.Equal(t, EvidenceDepthMetadata, fallback.Evidence)
	assert.NotEmpty(t, fallback.Labels())

	// An invented type resolves through the built-in, which agrees with the
	// shipped fallback: zero configuration, full vocabulary.
	invented := TypePolicyFor("research")
	assert.NotEmpty(t, invented.Labels())
	for _, label := range invented.Labels() {
		_, err := ResolveDisposition(invented, label)
		assert.NoError(t, err, "every offered label maps to a real action")
	}
}

// TestNamedBuiltinProfiles covers the three shipped profiles.
func TestNamedBuiltinProfiles(t *testing.T) {
	tests := []struct {
		name      string
		wantDepth EvidenceDepth
		wantBatch int
	}{
		{name: ProfileNameDefault, wantDepth: EvidenceDepthDeep, wantBatch: 5},
		{name: ProfileNameSweep, wantDepth: EvidenceDepthMetadata, wantBatch: 20},
		{name: ProfileNameDeep, wantDepth: EvidenceDepthDeep, wantBatch: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, err := BuiltinProfile(tt.name)
			require.NoError(t, err)
			assert.Equal(t, tt.wantBatch, profile.Review.BatchSize)
			assert.Equal(t, tt.wantDepth, profile.Evidence.DepthByStage["active"])
		})
	}

	// sweep reads nothing deeply; deep reads everything deeply.
	sweep, err := BuiltinProfile(ProfileNameSweep)
	require.NoError(t, err)
	for stage, depth := range sweep.Evidence.DepthByStage {
		assert.Equal(t, EvidenceDepthMetadata, depth, "stage %q", stage)
	}
	deep, err := BuiltinProfile(ProfileNameDeep)
	require.NoError(t, err)
	for stage, depth := range deep.Evidence.DepthByStage {
		assert.Equal(t, EvidenceDepthDeep, depth, "stage %q", stage)
	}
}

// TestUnknownProfileNamesTheOnesThatExist.
func TestUnknownProfileNamesTheOnesThatExist(t *testing.T) {
	_, err := BuiltinProfile("nonsense")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonsense")
	for _, name := range ProfileNames() {
		assert.Contains(t, err.Error(), name)
	}
}

// TestNamedProfileWinsOverTheCampaignFile: `--profile sweep` is a statement
// about this run, not about the campaign.
func TestNamedProfileWinsOverTheCampaignFile(t *testing.T) {
	root := scaffolded(t)

	resolution, err := ResolveProfileNamed(context.Background(), root, ProfileNameSweep)
	require.NoError(t, err)
	assert.False(t, resolution.FromFile)
	assert.Equal(t, ProfileNameSweep, resolution.Name)
	assert.Equal(t, 20, resolution.Profile.Review.BatchSize)
}

// TestMissingKeysInheritTheDefault is the merge rule: the file is edited down,
// not filled in.
func TestMissingKeysInheritTheDefault(t *testing.T) {
	root := scaffolded(t)
	writeProfile(t, root, "schema_version: triage-profile/v1alpha1\nreview:\n  batch_size: 42\n")

	resolution, err := ResolveProfileNamed(context.Background(), root, "")
	require.NoError(t, err)

	assert.Equal(t, 42, resolution.Profile.Review.BatchSize, "the declared key wins")
	assert.Equal(t, RunModeIncremental, resolution.Profile.Runs.Mode,
		"an omitted key inherits the built-in default")
	assert.Equal(t, GroupByType, resolution.Profile.Review.GroupBy,
		"omitted keys inside a declared block still inherit")
}

// TestAcceptance3_ValidationNamesFileKeyAndAllowedValues is the acceptance
// criterion in one test: every rejection points at the exact file, the exact
// key, and what would have worked.
func TestAcceptance3_ValidationNamesFileKeyAndAllowedValues(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantKey  string
		wantHint string
	}{
		{
			name:     "unknown key",
			body:     "schema_version: triage-profile/v1alpha1\nreview:\n  btch_size: 5\n",
			wantKey:  "btch_size",
			wantHint: "not found",
		},
		{
			name:     "bad enum",
			body:     "schema_version: triage-profile/v1alpha1\nruns:\n  mode: sideways\n",
			wantKey:  "runs.mode",
			wantHint: "incremental",
		},
		{
			name:     "bad grouping",
			body:     "schema_version: triage-profile/v1alpha1\nreview:\n  group_by: vibes\n",
			wantKey:  "review.group_by",
			wantHint: "attention_stage",
		},
		{
			name:     "batch size below one",
			body:     "schema_version: triage-profile/v1alpha1\nreview:\n  batch_size: 0\n",
			wantKey:  "review.batch_size",
			wantHint: "at least 1",
		},
		{
			name:     "negative anchor throttle",
			body:     "schema_version: triage-profile/v1alpha1\nanchors:\n  recheck_minutes: -1\n",
			wantKey:  "anchors.recheck_minutes",
			wantHint: "negative",
		},
		{
			name:     "export path escaping the campaign",
			body:     "schema_version: triage-profile/v1alpha1\noutputs:\n  priorities_export: ../outside.md\n",
			wantKey:  "outputs.priorities_export",
			wantHint: "inside the camp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := scaffolded(t)
			writeProfile(t, root, tt.body)

			_, err := ResolveProfileNamed(context.Background(), root, "")
			require.Error(t, err)

			message := err.Error()
			assert.Contains(t, message, "profile.yaml", "the error names the file")
			assert.Contains(t, message, tt.wantKey, "the error names the key")
			assert.Contains(t, message, tt.wantHint, "the error says what would work")
		})
	}
}

// TestTypePolicyDispositionsReplaceRatherThanMerge is the semantic that makes
// a type policy worth having: it must be able to RESTRICT the vocabulary.
func TestTypePolicyDispositionsReplaceRatherThanMerge(t *testing.T) {
	root := scaffolded(t)

	resolution, err := ResolveProfileNamed(context.Background(), root, "")
	require.NoError(t, err)

	design := resolution.TypePolicies["design"]
	assert.Equal(t, []string{"completed", "consolidate", "next", "parked"}, design.Labels(),
		"design offers exactly what its file declares, not that plus the built-ins")
	assert.Equal(t, EvidenceDepthDeep, design.Evidence)

	intent := resolution.TypePolicies["intent"]
	// No `parked`: camp refuses an attention stage on a file-backed workitem,
	// so the shipped policy no longer offers a label it could never execute.
	assert.Equal(t, []string{"active", "ready", "someday"}, intent.Labels())
	assert.Equal(t, EvidenceDepthMetadata, intent.Evidence,
		"an inbox decision is seconds, not a code trace")
}

// TestTypePolicyValidationRejectsAnUnmappableLabel is the failure that would
// otherwise surface at apply time, on a verdict someone already approved.
func TestTypePolicyValidationRejectsAnUnmappableLabel(t *testing.T) {
	root := scaffolded(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(root, scaffold.DirName, "types", "design.yaml"),
		[]byte("schema_version: triage-type/v1alpha1\nevidence: deep\ndispositions:\n  shipped: dungeon/nonsense\n"),
		0o644))

	_, err := ResolveProfileNamed(context.Background(), root, "")
	require.Error(t, err)

	message := err.Error()
	assert.Contains(t, message, "types/design.yaml")
	assert.Contains(t, message, "dispositions.shipped")
	assert.Contains(t, message, "not an action camp can perform")
	assert.Contains(t, message, "dungeon/completed", "the error lists what would work")
}

// TestTypePolicyWithNoVocabularyIsRejected: a type that can never be decided
// is a configuration mistake, not a valid restriction.
func TestTypePolicyWithNoVocabularyIsRejected(t *testing.T) {
	policy := TypePolicy{SchemaVersion: TypePolicySchemaVersion, Evidence: EvidenceDepthNone}
	err := ValidateTypePolicy(policy, "types/x.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can never be decided")
}

// TestResolveWithoutAScaffoldUsesBuiltins: a campaign that has not scaffolded
// still triages.
func TestResolveWithoutAScaffoldUsesBuiltins(t *testing.T) {
	resolution, err := ResolveProfileNamed(context.Background(), t.TempDir(), "")
	require.NoError(t, err)

	assert.False(t, resolution.FromFile)
	assert.Equal(t, ProfileNameDefault, resolution.Name)
	assert.Empty(t, resolution.TypePolicies)
	assert.Equal(t, RunModeIncremental, resolution.Profile.Runs.Mode)
}

// TestAnEmptyProfileFileIsLegal: deleting every key means "use the defaults".
func TestAnEmptyProfileFileIsLegal(t *testing.T) {
	root := scaffolded(t)
	writeProfile(t, root, "")

	resolution, err := ResolveProfileNamed(context.Background(), root, "")
	require.NoError(t, err)
	assert.Equal(t, DefaultProfile().Review.BatchSize, resolution.Profile.Review.BatchSize)
}

// TestResolveHonorsCancellation is the context rule.
func TestResolveHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ResolveProfileNamed(ctx, t.TempDir(), "")
	assert.ErrorIs(t, err, context.Canceled)
}
