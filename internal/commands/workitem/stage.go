package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
)

func newStageCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "stage <selector> <current|staged|active|parked|clear>",
		Short: "Set or clear the attention stage of a workitem",
		Args:  jsoncontract.Args(WorkitemStageJSONVersion, func() bool { return jsonOut }, cobra.ExactArgs(2)),
		RunE: jsoncontract.RunE(WorkitemStageJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runStage(cmd.Context(), cmd, args[0], args[1], jsonOut)
		}),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Sets workitem attention stage with --json output for automation",
		},
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemStageJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func parseAttentionStage(raw string) (priority.AttentionStage, bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "current":
		return priority.AttentionCurrent, false, nil
	case "staged":
		return priority.AttentionStaged, false, nil
	case "active":
		return priority.AttentionActive, false, nil
	case "parked":
		return priority.AttentionParked, false, nil
	case "clear", "none":
		return priority.AttentionNone, true, nil
	default:
		return priority.AttentionNone, false, camperrors.NewValidation("attention_stage",
			fmt.Sprintf("unknown attention stage %q (valid: current, staged, active, parked, clear)", raw), nil)
	}
}

func isValidAttentionStage(raw string) bool {
	stage, clear, err := parseAttentionStage(raw)
	return err == nil && !clear && stage.Valid()
}

func runStage(ctx context.Context, cmd *cobra.Command, selectorArg, stageArg string, jsonOut bool) error {
	stage, clear, err := parseAttentionStage(stageArg)
	if err != nil {
		return err
	}
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	wi, err := selector.Resolve(ctx, root, selectorArg, selector.ResolveOptions{})
	if err != nil {
		return err
	}
	if !priority.EligibleForAttention(*wi) {
		return camperrors.NewValidation("workitem", "attention stage is only supported for directory-backed workflow workitems", nil)
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return camperrors.Wrap(err, "discovering work items")
	}
	validKeys := priority.ValidKeys(items)
	storePath := priority.StorePath(root)
	if err := priority.WithLock(ctx, storePath, func(store *priority.Store) error {
		if clear {
			priority.ClearAttentionStage(store, wi.Key)
		} else {
			priority.SetAttentionStage(store, wi.Key, stage)
		}
		priority.Prune(store, validKeys)
		return nil
	}); err != nil {
		return camperrors.Wrap(err, "updating workitem attention stage")
	}
	if jsonOut {
		return emitStageJSON(cmd.OutOrStdout(), wi.Key, stage, clear)
	}
	if clear {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "cleared attention stage: %s\n", wi.Key)
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "attention stage set: %s = %s\n", wi.Key, stage)
	return err
}

func emitStageJSON(w io.Writer, key string, stage priority.AttentionStage, cleared bool) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		SchemaVersion  string    `json:"schema_version"`
		GeneratedAt    time.Time `json:"generated_at"`
		Key            string    `json:"key"`
		AttentionStage string    `json:"attention_stage"`
		Cleared        bool      `json:"cleared"`
	}{
		SchemaVersion:  WorkitemStageJSONVersion,
		GeneratedAt:    time.Now().UTC(),
		Key:            key,
		AttentionStage: string(stage),
		Cleared:        cleared,
	})
}
