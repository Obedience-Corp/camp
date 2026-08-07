package triage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// TemplateInput is what building an evidence template needs.
type TemplateInput struct {
	CampaignRoot string
	Row          ManifestRow
	// Item is the live workitem, when discovery still finds it. Nil is normal:
	// a row can outlive the thing it describes, and the template still works
	// from the frozen row.
	Item *workitem.WorkItem
	Now  time.Time
}

// BuildEvidenceTemplate returns a record with everything camp can establish
// already filled in, and the judgment fields left empty.
//
// The split is the point. Anchors and signals are facts camp measured; the
// empty fields are what a person or an agent has to decide. A template that
// guessed at `delivered` would produce a record asserting a conclusion nobody
// reached, which is the failure mode the advisory boundary exists to prevent.
func BuildEvidenceTemplate(ctx context.Context, in TemplateInput) (*EvidenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if in.Row.StableID == "" {
		return nil, camperrors.NewValidation("stable_id", "is required", nil)
	}

	record := &EvidenceRecord{
		SchemaVersion: SchemaVersion,
		StableID:      in.Row.StableID,
		Signals:       templateSignals(in),
		Anchors:       templateAnchors(ctx, in),
		ProducedBy: ProducedBy{
			Role:    EvidenceRoleDeterministic,
			Runtime: TemplateRuntime,
			At:      in.Now,
		},
	}
	record.Normalize()
	return record, nil
}

// TemplateRuntime names the producer of a pre-filled record, so a reader can
// tell a template apart from a record something actually read.
const TemplateRuntime = "camp triage evidence template"

// templateSignals collects the deterministic facts camp holds about a row.
// Values are strings because they are for a reader to weigh, not for camp to
// compute with; anything camp acts on mechanically is an anchor instead.
func templateSignals(in TemplateInput) map[string]string {
	signals := map[string]string{
		"type":            in.Row.Type,
		"lifecycle_stage": in.Row.LifecycleStage,
		"relative_path":   in.Row.RelativePath,
	}
	if in.Row.AttentionStage != "" {
		signals["attention_stage"] = in.Row.AttentionStage
	}
	if in.Row.IdentityException != nil {
		signals["identity"] = "path-bound: no .workitem marker"
	}

	item := in.Item
	if item == nil {
		return signals
	}
	if !item.UpdatedAt.IsZero() {
		signals["updated_at"] = item.UpdatedAt.UTC().Format(time.RFC3339)
		signals["days_since_update"] = strconv.Itoa(daysBetween(item.UpdatedAt, in.Now))
	}
	if !item.CreatedAt.IsZero() {
		signals["created_at"] = item.CreatedAt.UTC().Format(time.RFC3339)
		signals["age_days"] = strconv.Itoa(daysBetween(item.CreatedAt, in.Now))
	}
	if len(item.Tags) > 0 {
		signals["tags"] = joinSorted(item.Tags)
	}
	if len(item.Projects) > 0 {
		signals["projects"] = joinSorted(item.Projects)
	}
	if wf := item.WorkflowMeta; wf != nil {
		if wf.RunStatus != "" {
			signals["workflow_run_status"] = wf.RunStatus
		}
		if wf.LatestRunStatus != "" {
			signals["workflow_latest_run_status"] = wf.LatestRunStatus
		}
		if wf.TotalSteps > 0 {
			signals["workflow_progress"] = strconv.Itoa(wf.CompletedSteps) + "/" + strconv.Itoa(wf.TotalSteps)
		}
		if wf.Blocked {
			signals["workflow_blocked"] = "true"
		}
	}
	return signals
}

// templateAnchors pre-fills the facts refresh can mechanically re-check.
//
// No `pr` anchor is ever fabricated here: camp cannot observe a pull request
// without asking a remote, and an anchor asserting a state nobody checked
// would make a verdict look verified when it is not. PR anchors enter only
// when a driver asserts one.
func templateAnchors(ctx context.Context, in TemplateInput) []Anchor {
	anchors := []Anchor{}

	if in.Row.RelativePath != "" {
		anchor := Anchor{Kind: AnchorKindPath, Path: in.Row.RelativePath}
		if hash, err := hashPath(ctx, filepath.Join(in.CampaignRoot, filepath.FromSlash(in.Row.RelativePath))); err == nil {
			anchor.Hash = hash
			anchors = append(anchors, anchor)
		}
	}

	// The row's own stage, so a move or a promotion shows up as a change.
	stage := in.Row.AttentionStage
	if stage == "" {
		stage = in.Row.LifecycleStage
	}
	if stage != "" {
		anchors = append(anchors, Anchor{
			Kind:          AnchorKindWorkitem,
			StableID:      in.Row.StableID,
			ObservedStage: stage,
		})
	}

	if in.Item != nil && in.Item.WorkflowMeta != nil {
		if id := in.Item.WorkflowMeta.WorkflowID; id != "" {
			observed := in.Item.WorkflowMeta.RunStatus
			if observed == "" {
				observed = in.Item.WorkflowMeta.LatestRunStatus
			}
			if observed == "" {
				observed = ObservedUncheckedOffline
			}
			anchors = append(anchors, Anchor{
				Kind:     AnchorKindFestival,
				ID:       id,
				Observed: observed,
			})
		}
	}
	return anchors
}

// hashPath returns the content hash of a file, or of a directory's primary
// document when the row is a directory workitem.
func hashPath(ctx context.Context, abs string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	target := abs
	if info.IsDir() {
		// Hash the marker rather than walking the tree: it is what identity
		// and lineage live in, and a whole-directory hash would churn on every
		// unrelated edit, making the anchor useless as a staleness signal.
		target = filepath.Join(abs, workitem.MetadataFilename)
		if _, err := os.Stat(target); err != nil {
			return "", err
		}
	}

	f, err := os.Open(target)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return PathHashPrefix + hex.EncodeToString(sum.Sum(nil)), nil
}

// daysBetween returns whole days from then to now, floored at zero.
func daysBetween(then, now time.Time) int {
	days := int(now.Sub(then).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// joinSorted renders a set as a stable comma-separated string.
func joinSorted(values []string) string {
	sorted := sortedCopy(values)
	out := ""
	for i, v := range sorted {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}
