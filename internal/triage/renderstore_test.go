package triage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderStore returns a store holding a run whose first row is fully judged.
func renderStore(t *testing.T) (*Store, string, *Run) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store := NewStore(root, func() time.Time { return testAt })

	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	target := run.Manifest.Rows[0].StableID
	record := validEvidence()
	record.StableID = target
	_, err = store.WriteEvidence(ctx, run.ID, record)
	require.NoError(t, err)
	_, err = store.Propose(ctx, ProposeInput{
		RunID: run.ID, StableID: target, Disposition: "completed",
		Rationale: rationale("delivered in PR 239"), Actor: "tester", Now: testAt,
	})
	require.NoError(t, err)

	return store, root, run
}

// --- export path safety, error cases first -----------------------------

// TestResolveExportPathRefusesEscapes: the profile is a file a user edits, so
// its export path is untrusted input reaching a write. A silently relocated
// brief would be one the user never finds.
func TestResolveExportPathRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name string
		rel  string
	}{
		{"absolute path", "/tmp/escaped.md"},
		{"parent traversal", "../escaped.md"},
		{"deep traversal", "docs/../../escaped.md"},
		{"bare parent", ".."},
		{"empty", "   "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveExportPath(root, tc.rel)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "priorities_export",
				"the refusal names the profile key that has to change")
		})
	}
}

// TestResolveExportPathAcceptsCampaignRelative covers the working cases.
func TestResolveExportPathAcceptsCampaignRelative(t *testing.T) {
	root := t.TempDir()
	tests := []struct{ rel, want string }{
		{"PRIORITIES.md", "PRIORITIES.md"},
		{"current_priorities/PRIORITIES.md", "current_priorities/PRIORITIES.md"},
		{"./docs/brief.md", "docs/brief.md"},
		{"docs/../docs/brief.md", "docs/brief.md"},
	}

	for _, tc := range tests {
		t.Run(tc.rel, func(t *testing.T) {
			abs, err := ResolveExportPath(root, tc.rel)

			require.NoError(t, err)
			assert.Equal(t, filepath.Join(root, filepath.FromSlash(tc.want)), abs)
		})
	}
}

// --- loading -----------------------------------------------------------

// TestLoadRenderInputGathersRunData proves the renderers get what they need
// from the store without a second pass.
func TestLoadRenderInputGathersRunData(t *testing.T) {
	ctx := context.Background()
	store, _, run := renderStore(t)

	in, err := LoadRenderInput(ctx, store, run.ID)

	require.NoError(t, err)
	assert.Equal(t, run.ID, in.Run.ID)
	assert.Len(t, in.Run.Manifest.Rows, len(run.Manifest.Rows))

	target := run.Manifest.Rows[0].StableID
	assert.Equal(t, VerdictProposed, in.Verdicts[target].State)
	require.Contains(t, in.Rationales, target)
	assert.Equal(t, "delivered in PR 239", in.Rationales[target].Summary)
	assert.Equal(t, 1, in.EvidenceRoles[EvidenceRoleEvidence])
	assert.Equal(t, 0, in.NoEvidenceCount)
}

// TestLoadRenderInputCountsNoEvidenceSeparately keeps "judged without a
// gathered record" distinct from "read by something".
func TestLoadRenderInputCountsNoEvidenceSeparately(t *testing.T) {
	ctx := context.Background()
	store, _, run := renderStore(t)
	marker := &EvidenceRecord{
		SchemaVersion: SchemaVersion,
		StableID:      run.Manifest.Rows[1].StableID,
		NoEvidence:    true,
		ProducedBy:    ProducedBy{Role: EvidenceRoleHuman, Runtime: "human", At: testAt},
	}
	_, err := store.WriteEvidence(ctx, run.ID, marker)
	require.NoError(t, err)

	in, err := LoadRenderInput(ctx, store, run.ID)

	require.NoError(t, err)
	assert.Equal(t, 1, in.NoEvidenceCount)
	assert.Equal(t, 1, in.EvidenceRoles[EvidenceRoleEvidence])
	assert.Equal(t, 0, in.EvidenceRoles[EvidenceRoleHuman],
		"a no-evidence marker is not a reading by a human")
}

// TestLoadRenderInputSurvivesACorruptRecord: the review document is how an
// operator sees the run, so one bad file inside it must not make the run
// unviewable.
func TestLoadRenderInputSurvivesACorruptRecord(t *testing.T) {
	ctx := context.Background()
	store, _, run := renderStore(t)
	target := run.Manifest.Rows[0].StableID
	require.NoError(t, os.WriteFile(store.EvidencePath(run.ID, target), []byte("{ not json"), 0o644))

	in, err := LoadRenderInput(ctx, store, run.ID)

	require.NoError(t, err, "a corrupt record must not break the render")
	assert.Equal(t, 0, in.EvidenceRoles[EvidenceRoleEvidence])
	assert.NotEmpty(t, RenderReview(in), "the document still renders")
}

// TestRationaleMissingIsNotAnError: a row can hold a verdict recorded before
// rationales existed, or none at all.
func TestRationaleMissingIsNotAnError(t *testing.T) {
	ctx := context.Background()
	store, _, run := renderStore(t)

	got, err := store.Rationale(ctx, run.ID, "never-proposed")

	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestLoadRenderInputRespectsCancellation stops before reading.
func TestLoadRenderInputRespectsCancellation(t *testing.T) {
	store, _, run := renderStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := LoadRenderInput(ctx, store, run.ID)

	assert.ErrorIs(t, err, context.Canceled)
}

// --- writing -----------------------------------------------------------

// TestRenderDocumentsWritesIntoTheRun covers the write path end to end.
func TestRenderDocumentsWritesIntoTheRun(t *testing.T) {
	ctx := context.Background()
	store, _, run := renderStore(t)

	result, err := store.RenderDocuments(ctx, run.ID)

	require.NoError(t, err)
	assert.Equal(t, len(run.Manifest.Rows), result.Rows)
	assert.GreaterOrEqual(t, result.Lanes, 1)

	review, err := os.ReadFile(result.ReviewPath)
	require.NoError(t, err)
	assert.Contains(t, string(review), "# Triage review — "+run.ID)
	priorities, err := os.ReadFile(result.PrioritiesPath)
	require.NoError(t, err)
	assert.Contains(t, string(priorities), "# Portfolio priorities")
}

// TestRenderDocumentsOverwritesEdits is D3 at the store boundary: the document
// is output, so an edit is discarded rather than honored.
func TestRenderDocumentsOverwritesEdits(t *testing.T) {
	ctx := context.Background()
	store, _, run := renderStore(t)
	result, err := store.RenderDocuments(ctx, run.ID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(result.ReviewPath, []byte("APPROVED BY HAND\n"), 0o644))

	_, err = store.RenderDocuments(ctx, run.ID)

	require.NoError(t, err)
	review, err := os.ReadFile(result.ReviewPath)
	require.NoError(t, err)
	assert.NotContains(t, string(review), "APPROVED BY HAND")
}

// TestExportPrioritiesWithNoPathIsANoOp: the default profile configures no
// export, and that is a choice rather than a failure.
func TestExportPrioritiesWithNoPathIsANoOp(t *testing.T) {
	ctx := context.Background()
	store, root, run := renderStore(t)

	path, err := store.ExportPriorities(ctx, root, run.ID)

	require.NoError(t, err)
	assert.Empty(t, path)
}

// TestExportPrioritiesWritesAndOverwrites covers the D3 overwrite rule.
func TestExportPrioritiesWritesAndOverwrites(t *testing.T) {
	ctx := context.Background()
	store, root, run := renderStore(t)
	run.Manifest.Profile.Resolved.Outputs.PrioritiesExport = "current_priorities/PRIORITIES.md"
	body, err := MarshalDocument(run.Manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(run.Dir, ManifestFileName), body, 0o644))

	first, err := store.ExportPriorities(ctx, root, run.ID)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "current_priorities", "PRIORITIES.md"), first)
	written, err := os.ReadFile(first)
	require.NoError(t, err)
	assert.Contains(t, string(written), "# Portfolio priorities")

	second, err := store.ExportPriorities(ctx, root, run.ID)
	require.NoError(t, err)
	assert.Equal(t, first, second, "the export overwrites in place, never versions")

	entries, err := os.ReadDir(filepath.Dir(first))
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

// TestExportPrioritiesRefusesAnEscapingPath stops a hand-edited profile from
// writing outside the campaign.
func TestExportPrioritiesRefusesAnEscapingPath(t *testing.T) {
	ctx := context.Background()
	store, root, run := renderStore(t)
	run.Manifest.Profile.Resolved.Outputs.PrioritiesExport = "../escaped/PRIORITIES.md"
	body, err := MarshalDocument(run.Manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(run.Dir, ManifestFileName), body, 0o644))

	_, err = store.ExportPriorities(ctx, root, run.ID)

	require.Error(t, err)
	require.ErrorIs(t, err, camperrors.ErrInvalidInput)
	assert.Contains(t, err.Error(), "priorities_export")
	_, statErr := os.Stat(filepath.Join(filepath.Dir(root), "escaped"))
	assert.True(t, os.IsNotExist(statErr), "nothing is written outside the campaign")
}

// --- lane coverage -----------------------------------------------------

// TestDungeonLanesSeparateByTarget: completed, archived, and someday are
// different outcomes and must not collapse into one section.
func TestDungeonLanesSeparateByTarget(t *testing.T) {
	tests := []struct {
		action CanonicalAction
		title  string
	}{
		{"dungeon/completed", "Close as delivered"},
		{"dungeon/archived", "Archive"},
		{"dungeon/someday", "Someday"},
	}

	seen := map[int]string{}
	for _, tc := range tests {
		t.Run(string(tc.action), func(t *testing.T) {
			spec := laneSpecFor(RowVerdict{State: VerdictApproved, CanonicalAction: tc.action})

			assert.Equal(t, tc.title, spec.title)
			_, clash := seen[spec.key]
			assert.False(t, clash, "each dungeon target gets its own lane")
			seen[spec.key] = tc.title
		})
	}
}
