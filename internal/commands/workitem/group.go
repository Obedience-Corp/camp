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

func newGroupCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "group <selector> <group|clear>",
		Short: "Set or clear the group of a workitem",
		Args:  jsoncontract.Args(WorkitemGroupJSONVersion, func() bool { return jsonOut }, cobra.ExactArgs(2)),
		RunE: jsoncontract.RunE(WorkitemGroupJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runGroup(cmd.Context(), cmd, args[0], args[1], jsonOut)
		}),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Sets workitem group with --json output for automation",
		},
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemGroupJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func parseGroup(raw string) (group string, clear bool, err error) {
	group = strings.ToLower(strings.TrimSpace(raw))
	if group == "clear" || group == "none" {
		return "", true, nil
	}
	if !priority.ValidGroup(group) {
		return "", false, camperrors.NewValidation("group",
			fmt.Sprintf("invalid group %q (use lowercase letters, numbers, dash, or underscore; no leading dash or dot; max 80 chars)", raw), nil)
	}
	return group, false, nil
}

func runGroup(ctx context.Context, cmd *cobra.Command, selectorArg, groupArg string, jsonOut bool) error {
	group, clear, err := parseGroup(groupArg)
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
		return camperrors.NewValidation("workitem", "group is only supported for directory-backed workflow workitems", nil)
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
			priority.ClearGroup(store, wi.Key)
		} else {
			priority.SetGroup(store, wi.Key, group)
		}
		priority.Prune(store, validKeys)
		return nil
	}); err != nil {
		return camperrors.Wrap(err, "updating workitem group")
	}
	if jsonOut {
		return emitGroupJSON(cmd.OutOrStdout(), wi.Key, group, clear)
	}
	if clear {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "cleared group: %s\n", wi.Key)
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "group set: %s = %s\n", wi.Key, group)
	return err
}

func emitGroupJSON(w io.Writer, key, group string, cleared bool) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		SchemaVersion string    `json:"schema_version"`
		GeneratedAt   time.Time `json:"generated_at"`
		Key           string    `json:"key"`
		Group         string    `json:"group"`
		Cleared       bool      `json:"cleared"`
	}{
		SchemaVersion: WorkitemGroupJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		Key:           key,
		Group:         group,
		Cleared:       cleared,
	})
}
