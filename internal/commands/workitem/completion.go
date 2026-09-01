package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	commit "github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ledger"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

type completionDecision string

const (
	completionReview      completionDecision = "review"
	completionAcknowledge completionDecision = "acknowledge"
	completionRecurring   completionDecision = "recurring"
)

type completionSnapshot struct {
	Policy        wkitem.CompletionPolicy `json:"policy"`
	ReviewedRunID string                  `json:"reviewed_run_id,omitempty"`
}

type completionResult struct {
	SchemaVersion string             `json:"schema_version"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Workitem      completionIdentity `json:"workitem"`
	Before        completionSnapshot `json:"before"`
	After         completionSnapshot `json:"after"`
	Changed       bool               `json:"changed"`
	Adopted       bool               `json:"adopted,omitempty"`
	Committed     bool               `json:"committed"`
	Deferred      bool               `json:"deferred,omitempty"`
}

type completionIdentity struct {
	ID   string `json:"id"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path"`
}

func newCompletionCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "completion <selector> <review|acknowledge|recurring>",
		Short: "Set completed-run review behavior for a workitem",
		Long: `Set how completed standalone workflow runs affect one workitem.

review restores the default and clears any one-run acknowledgement.
acknowledge keeps the workitem active and suppresses only its latest completed
run. recurring keeps the workitem active and suppresses every completed-run
review until review is restored. Persistent decisions live in the versioned
.workitem marker and apply to both camp fresh and camp workitem sweep.`,
		Args: jsoncontract.Args(WorkitemCompletionJSONVersion, func() bool { return jsonOut }, cobra.ExactArgs(2)),
		RunE: jsoncontract.RunE(WorkitemCompletionJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			decision, err := parseCompletionDecision(args[1])
			if err != nil {
				return err
			}
			result, err := setWorkitemCompletion(cmd.Context(), cmd, args[0], decision)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitCompletionJSON(cmd.OutOrStdout(), result)
			}
			return emitCompletionHuman(cmd.OutOrStdout(), result, decision)
		}),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Explicit reversible workitem lifecycle decision with structured output",
		},
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemCompletionJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func parseCompletionDecision(raw string) (completionDecision, error) {
	switch completionDecision(strings.ToLower(strings.TrimSpace(raw))) {
	case completionReview:
		return completionReview, nil
	case completionAcknowledge:
		return completionAcknowledge, nil
	case completionRecurring:
		return completionRecurring, nil
	default:
		return "", camperrors.NewValidation("completion",
			fmt.Sprintf("unknown decision %q (valid: review, acknowledge, recurring)", raw), nil)
	}
}

func completionState(meta *wkitem.Metadata) completionSnapshot {
	policy := wkitem.CompletionPolicyReview
	if meta != nil && meta.CompletionPolicy != "" {
		policy = meta.CompletionPolicy
	}
	state := completionSnapshot{Policy: policy}
	if meta != nil {
		state.ReviewedRunID = meta.CompletionReviewedRunID
	}
	return state
}

func applyCompletionDecision(meta *wkitem.Metadata, decision completionDecision, workflow *wkitem.WorkItemWorkflow) (completionSnapshot, completionSnapshot, bool, error) {
	before := completionState(meta)
	var after completionSnapshot
	switch decision {
	case completionReview:
		after = completionSnapshot{Policy: wkitem.CompletionPolicyReview}
	case completionRecurring:
		after = completionSnapshot{Policy: wkitem.CompletionPolicyRecurring}
	case completionAcknowledge:
		if workflow == nil || workflow.ActiveRunID != "" || workflow.LatestRunStatus != "completed" || workflow.LatestRunID == "" {
			return before, before, false, camperrors.NewValidation("completion",
				"acknowledge requires a latest completed workflow run and no active run", nil)
		}
		after = completionSnapshot{Policy: wkitem.CompletionPolicyReview, ReviewedRunID: workflow.LatestRunID}
	default:
		return before, before, false, camperrors.NewValidation("completion", "unsupported decision", nil)
	}
	changed := before != after
	if meta != nil && decision == completionReview && meta.CompletionPolicy == wkitem.CompletionPolicyReview {
		changed = true // canonicalize explicit default to omission
	}
	return before, after, changed, nil
}

func setWorkitemCompletion(ctx context.Context, cmd *cobra.Command, selectorArg string, decision completionDecision) (*completionResult, error) {
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "not in a camp directory")
	}
	wi, err := selector.Resolve(ctx, root, selectorArg, selector.ResolveOptions{})
	if err != nil {
		return nil, err
	}
	if wi.ItemKind != wkitem.ItemKindDirectory || wi.WorkflowType == wkitem.WorkflowTypeFestival || wi.WorkflowType == wkitem.WorkflowTypeIntent {
		return nil, camperrors.NewValidation("workitem", "completion policy is only supported for directory-backed workflow workitems", nil)
	}

	markerPath := filepath.Join(root, filepath.FromSlash(wi.RelativePath), wkitem.MetadataFilename)
	meta, err := wkitem.LoadMetadata(ctx, wi.AbsPath(root))
	if err != nil {
		return nil, err
	}
	before, after, changed, err := applyCompletionDecision(meta, decision, wi.WorkflowMeta)
	if err != nil {
		return nil, err
	}
	result := &completionResult{
		SchemaVersion: WorkitemCompletionJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		Workitem:      completionIdentity{ID: wi.StableID, Ref: refOfItem(*wi), Path: filepath.ToSlash(wi.RelativePath)},
		Before:        before, After: after, Changed: changed,
	}
	if !changed && meta != nil {
		return result, nil
	}
	if meta == nil {
		if decision == completionReview {
			return result, nil
		}
		id, genErr := generateID(ctx, string(wi.WorkflowType), filepath.Base(wi.RelativePath), "", root)
		if genErr != nil {
			return nil, genErr
		}
		ref, refErr := deriveUniqueRef(ctx, root, cfg, id)
		if refErr != nil {
			return nil, refErr
		}
		adopted, adoptErr := wkitem.AdoptDirectory(ctx, root, wkitem.AdoptRequest{
			RelPath: wi.RelativePath, Type: string(wi.WorkflowType), Title: wi.Title, ID: id, Ref: ref,
		})
		if adoptErr != nil {
			return nil, adoptErr
		}
		meta = &adopted
		result.Adopted = true
		result.Workitem.ID, result.Workitem.Ref = id, ref
		appendWorkitemAuditEvent(ctx, cmd, root, wkaudit.Event{
			Event: wkaudit.EventAdopt, ID: id, Ref: ref, Type: string(wi.WorkflowType), Title: wi.Title, To: wi.RelativePath,
		})
	}

	meta.Version = wkitem.WorkitemSchemaVersion
	if after.Policy == wkitem.CompletionPolicyRecurring {
		meta.CompletionPolicy = wkitem.CompletionPolicyRecurring
	} else {
		meta.CompletionPolicy = ""
	}
	meta.CompletionReviewedRunID = after.ReviewedRunID
	if err := writeMarker(filepath.Dir(markerPath), *meta); err != nil {
		return nil, err
	}
	invalidateNavigationCache(cmd, root)

	appendWorkitemAuditEvent(ctx, cmd, root, wkaudit.Event{
		Event: wkaudit.EventDecide, ID: meta.ID, Ref: meta.Ref, Type: meta.Type, Title: meta.Title,
		From: wi.RelativePath, Target: "completion", Decision: string(decision), RunID: after.ReviewedRunID,
	})
	emitter := ledger.NewFromRoot(ctx, root, ledger.WarnTo(cmd.ErrOrStderr()))
	emitter.Emit(ctx, ledgerkit.KindDecided, ledgerkit.Scope{Workitem: meta.ID},
		ledger.WithWhy(completionCommitTitle(decision, meta.Ref, after.ReviewedRunID)),
		ledger.WithPayload(map[string]any{
			"target": "completion", "decision": string(decision),
			"before_policy": string(before.Policy), "after_policy": string(after.Policy),
			"before_reviewed_run_id": before.ReviewedRunID, "after_reviewed_run_id": after.ReviewedRunID,
		}))

	files := []string{markerPath, filepath.Join(root, ".campaign", "workitems", wkaudit.AuditFile)}
	res := commit.Workitem(ctx, commit.WorkitemOptions{
		Options: commit.Options{
			CampaignRoot: root, CampaignID: cfg.ID,
			Files: commit.NormalizeFiles(root, files...), SelectiveOnly: true,
		},
		Action: commit.WorkitemEdit, WorkitemID: meta.ID, WorkitemRef: meta.Ref,
		Title: completionCommitTitle(decision, meta.Ref, after.ReviewedRunID),
	})
	result.Committed, result.Deferred = res.Committed, res.Deferred
	if res.Err != nil {
		return nil, camperrors.Wrap(res.Err, "committing workitem completion decision")
	}
	if res.Skipped {
		return nil, camperrors.New(res.SkipReason)
	}
	return result, nil
}

func completionCommitTitle(decision completionDecision, ref, runID string) string {
	switch decision {
	case completionAcknowledge:
		return fmt.Sprintf("acknowledge completed run %s for %s", runID, ref)
	case completionRecurring:
		return fmt.Sprintf("set recurring completion policy for %s", ref)
	default:
		return fmt.Sprintf("restore completion review for %s", ref)
	}
}

func emitCompletionJSON(w io.Writer, result *completionResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func emitCompletionHuman(w io.Writer, result *completionResult, decision completionDecision) error {
	if !result.Changed {
		_, err := fmt.Fprintf(w, "completion unchanged: %s is already %s\n", result.Workitem.Path, result.After.Policy)
		return err
	}
	if result.Adopted {
		if _, err := fmt.Fprintf(w, "adopted %s\n  id: %s\n  ref: %s\n", result.Workitem.Path, result.Workitem.ID, result.Workitem.Ref); err != nil {
			return err
		}
	}
	switch decision {
	case completionAcknowledge:
		_, err := fmt.Fprintf(w, "acknowledged completed run %s for %s\n", result.After.ReviewedRunID, result.Workitem.Ref)
		return err
	case completionRecurring:
		_, err := fmt.Fprintf(w, "completion policy set: %s = recurring\n", result.Workitem.Ref)
		return err
	default:
		_, err := fmt.Fprintf(w, "completion review restored: %s\n", result.Workitem.Ref)
		return err
	}
}
