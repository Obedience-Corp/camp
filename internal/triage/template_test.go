package triage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// templateRow is a manifest row for a directory workitem on disk.
func templateRow() ManifestRow {
	return ManifestRow{
		StableID:       "design-hub",
		Ref:            workitem.Derive("design-hub"),
		Key:            "design:workflow/design/hub",
		Type:           "design",
		Title:          "Hub control plane",
		RelativePath:   "workflow/design/hub",
		LifecycleStage: "active",
		AttentionStage: "next",
		Batch:          1,
		Policy:         RowPolicy{Evidence: EvidenceDepthDeep, RoutingTier: RoutingTierDefault},
	}
}

// writeRowOnDisk materializes a row's directory with a marker so the path
// anchor has something real to hash.
func writeRowOnDisk(t *testing.T, root string, row ManifestRow) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(row.RelativePath))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, workitem.MetadataFilename),
		[]byte("version: v1alpha8\nkind: workitem\nid: "+row.StableID+"\ntype: design\n"), 0o644))
}

// --- error cases first -------------------------------------------------

// TestBuildEvidenceTemplateRequiresARow guards the nil path.
func TestBuildEvidenceTemplateRequiresARow(t *testing.T) {
	_, err := BuildEvidenceTemplate(context.Background(), TemplateInput{Now: testAt})

	assert.Error(t, err)
}

// TestBuildEvidenceTemplateRespectsCancellation stops before hashing.
func TestBuildEvidenceTemplateRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := BuildEvidenceTemplate(ctx, TemplateInput{Row: templateRow(), Now: testAt})

	assert.ErrorIs(t, err, context.Canceled)
}

// --- the fact/judgment split -------------------------------------------

// TestTemplateLeavesJudgmentEmpty is the whole contract. A template that
// guessed at what was delivered would produce a record asserting a conclusion
// nobody reached.
func TestTemplateLeavesJudgmentEmpty(t *testing.T) {
	root := t.TempDir()
	row := templateRow()
	writeRowOnDisk(t, root, row)

	record, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Now: testAt,
	})

	require.NoError(t, err)
	assert.Empty(t, record.OriginalGoal)
	assert.Empty(t, record.Delivered)
	assert.Empty(t, record.Missing)
	assert.Empty(t, record.StaleAssumptions)
	assert.Empty(t, record.OpenDecisions)
	assert.Empty(t, string(record.Confidence))
	assert.Empty(t, record.ConfidenceNotes)
}

// TestTemplateFillsTheFactsCampHolds is the other half: the parts camp can
// establish are already there, so the reader only supplies judgment.
func TestTemplateFillsTheFactsCampHolds(t *testing.T) {
	root := t.TempDir()
	row := templateRow()
	writeRowOnDisk(t, root, row)

	record, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Now: testAt,
	})

	require.NoError(t, err)
	assert.Equal(t, row.StableID, record.StableID)
	assert.Equal(t, SchemaVersion, record.SchemaVersion)
	assert.Equal(t, EvidenceRoleDeterministic, record.ProducedBy.Role,
		"a template is camp's own output, not a reading")
	assert.Equal(t, TemplateRuntime, record.ProducedBy.Runtime)

	assert.Equal(t, "design", record.Signals["type"])
	assert.Equal(t, "active", record.Signals["lifecycle_stage"])
	assert.Equal(t, "next", record.Signals["attention_stage"])
	assert.Equal(t, row.RelativePath, record.Signals["relative_path"])
}

// TestTemplateHashesThePathAnchor: the anchor is what lets refresh notice the
// content moved on.
func TestTemplateHashesThePathAnchor(t *testing.T) {
	root := t.TempDir()
	row := templateRow()
	writeRowOnDisk(t, root, row)

	record, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Now: testAt,
	})

	require.NoError(t, err)
	var path *Anchor
	for i := range record.Anchors {
		if record.Anchors[i].Kind == AnchorKindPath {
			path = &record.Anchors[i]
		}
	}
	require.NotNil(t, path, "a template must anchor the row's own content")
	assert.Equal(t, row.RelativePath, path.Path)
	assert.Contains(t, path.Hash, PathHashPrefix)
	assert.Empty(t, path.Validate(), "the pre-filled anchor must itself be valid")
}

// TestTemplateHashChangesWithContent proves the anchor is a real measurement
// rather than a placeholder.
func TestTemplateHashChangesWithContent(t *testing.T) {
	root := t.TempDir()
	row := templateRow()
	writeRowOnDisk(t, root, row)

	first, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Now: testAt,
	})
	require.NoError(t, err)

	marker := filepath.Join(root, filepath.FromSlash(row.RelativePath), workitem.MetadataFilename)
	require.NoError(t, os.WriteFile(marker, []byte("version: v1alpha8\nkind: workitem\nid: changed\n"), 0o644))

	second, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Now: testAt,
	})
	require.NoError(t, err)

	assert.NotEqual(t, first.Anchors[0].Hash, second.Anchors[0].Hash)
}

// TestTemplateNeverFabricatesAPRAnchor: camp cannot observe a pull request
// without asking a remote, and an anchor claiming a state nobody checked would
// make a verdict look verified when it is not.
func TestTemplateNeverFabricatesAPRAnchor(t *testing.T) {
	root := t.TempDir()
	row := templateRow()
	writeRowOnDisk(t, root, row)
	live := item("design-hub", "design", row.Key, "active", "next")
	live.SourceMetadata = map[string]any{"ref": row.Ref, "pr": "obey#239"}

	record, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Item: &live, Now: testAt,
	})

	require.NoError(t, err)
	for _, anchor := range record.Anchors {
		assert.NotEqual(t, AnchorKindPR, anchor.Kind)
	}
}

// TestTemplateAddsLiveSignalsWhenDiscoveryFindsTheRow covers the enrichment
// the frozen row cannot supply on its own.
func TestTemplateAddsLiveSignalsWhenDiscoveryFindsTheRow(t *testing.T) {
	root := t.TempDir()
	row := templateRow()
	writeRowOnDisk(t, root, row)

	live := item("design-hub", "design", row.Key, "active", "next")
	live.CreatedAt = testAt.Add(-30 * 24 * time.Hour)
	live.UpdatedAt = testAt.Add(-3 * 24 * time.Hour)
	live.Tags = []string{"zeta", "alpha"}
	live.WorkflowMeta = &workitem.WorkItemWorkflow{
		WorkflowID:      "CI0009",
		RunStatus:       "active",
		CompletedSteps:  2,
		TotalSteps:      5,
		Blocked:         true,
		LatestRunStatus: "running",
	}

	record, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Item: &live, Now: testAt,
	})

	require.NoError(t, err)
	assert.Equal(t, "30", record.Signals["age_days"])
	assert.Equal(t, "3", record.Signals["days_since_update"])
	assert.Equal(t, "alpha,zeta", record.Signals["tags"], "rendered in a stable order")
	assert.Equal(t, "active", record.Signals["workflow_run_status"])
	assert.Equal(t, "2/5", record.Signals["workflow_progress"])
	assert.Equal(t, "true", record.Signals["workflow_blocked"])

	var festival *Anchor
	for i := range record.Anchors {
		if record.Anchors[i].Kind == AnchorKindFestival {
			festival = &record.Anchors[i]
		}
	}
	require.NotNil(t, festival, "a linked festival is a re-checkable fact")
	assert.Equal(t, "CI0009", festival.ID)
	assert.Equal(t, "active", festival.Observed)
}

// TestTemplateWorksWithoutTheLiveItem: a row can outlive the thing it
// describes, and the template still has to work.
func TestTemplateWorksWithoutTheLiveItem(t *testing.T) {
	root := t.TempDir()
	row := templateRow()

	record, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Now: testAt,
	})

	require.NoError(t, err)
	assert.Equal(t, row.StableID, record.StableID)
	assert.Empty(t, record.Signals["age_days"])
	for _, anchor := range record.Anchors {
		assert.NotEqual(t, AnchorKindPath, anchor.Kind,
			"nothing on disk means no path anchor rather than a fabricated hash")
	}
}

// TestTemplateFlagsPathBoundIdentity so a reader knows the row has no durable
// id before deciding to move it.
func TestTemplateFlagsPathBoundIdentity(t *testing.T) {
	root := t.TempDir()
	row := templateRow()
	row.IdentityException = &IdentityException{
		Reason: "no marker", Path: row.RelativePath,
		GrantedBy: "camp-triage-preflight", GrantedAt: testAt,
	}

	record, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Now: testAt,
	})

	require.NoError(t, err)
	assert.Contains(t, record.Signals["identity"], "path-bound")
}

// --- encoding ----------------------------------------------------------

// TestMarshalTemplateSkipsValidation is why MarshalTemplate exists: a template
// is deliberately incomplete, so the validating encoder would refuse to emit
// the very form the reader is supposed to fill in.
func TestMarshalTemplateSkipsValidation(t *testing.T) {
	root := t.TempDir()
	row := templateRow()
	writeRowOnDisk(t, root, row)
	record, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Now: testAt,
	})
	require.NoError(t, err)

	require.NotEmpty(t, record.Validate(), "the template is intentionally invalid")

	_, err = MarshalDocument(record)
	require.Error(t, err, "the disk encoder must still refuse it")

	body, err := MarshalTemplate(record)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"stable_id": "design-hub"`)
	assert.Contains(t, string(body), `"signals"`)
}

// TestTemplateRoundTripsOnceJudgmentIsAdded closes the loop the command
// promises: template out, fill in, submit back.
func TestTemplateRoundTripsOnceJudgmentIsAdded(t *testing.T) {
	root := t.TempDir()
	row := templateRow()
	writeRowOnDisk(t, root, row)
	record, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Now: testAt,
	})
	require.NoError(t, err)

	body, err := MarshalTemplate(record)
	require.NoError(t, err)

	// Edit the JSON the way a person or an agent would, then submit it back
	// through the same strict path `camp triage evidence set` uses.
	var edited map[string]any
	require.NoError(t, json.Unmarshal(body, &edited))
	edited["original_goal"] = "Ship the hub control plane"
	edited["confidence"] = "medium"
	edited["produced_by"].(map[string]any)["role"] = "human"
	edited["produced_by"].(map[string]any)["runtime"] = "human"

	var filled EvidenceRecord
	require.NoError(t, ParseDocument(mustMarshal(t, edited), &filled, Strict))

	stored, err := MarshalDocument(&filled)
	require.NoError(t, err)
	assert.Contains(t, string(stored), "Ship the hub control plane")
	assert.Contains(t, string(stored), `"signals"`, "camp's facts survive the round trip")
	assert.Contains(t, string(stored), PathHashPrefix, "and so does the anchor it measured")
}

// TestSubmittingAnUnfilledTemplateSaysWhatToFillIn: handing the blank form
// straight back is the obvious mistake, so the refusal has to name the two
// fields that make it judgment rather than just facts.
func TestSubmittingAnUnfilledTemplateSaysWhatToFillIn(t *testing.T) {
	root := t.TempDir()
	row := templateRow()
	writeRowOnDisk(t, root, row)
	record, err := BuildEvidenceTemplate(context.Background(), TemplateInput{
		CampaignRoot: root, Row: row, Now: testAt,
	})
	require.NoError(t, err)
	body, err := MarshalTemplate(record)
	require.NoError(t, err)

	err = ParseDocument(body, &EvidenceRecord{}, Strict)

	require.Error(t, err)
	assert.ElementsMatch(t, []string{"original_goal", "confidence"}, violatedFields(err))
	assert.Contains(t, err.Error(), "high, medium, low")
}
