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
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
)

func newPriorityCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "priority <selector> <high|medium|low|clear>",
		Short: "Set or clear the manual priority of a workitem",
		Long: `Set or clear the manual priority of a workitem.

The selector accepts the same forms as 'camp workitem current': a stable
.workitem id, the workitem key (<type>:<path>), a relative path, or a directory
slug. Priority is one of high, medium, low, or clear (clear removes any manual
priority). Assignments persist in .campaign/settings/workitems.json, the same
store the interactive dashboard writes.

Examples:
  camp workitem priority festival:festivals/active/demo high
  camp workitem priority demo clear
  camp workitem priority demo high --json`,
		Args: jsoncontract.Args(WorkitemPriorityJSONVersion, func() bool { return jsonOut }, cobra.ExactArgs(2)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Sets workitem priority with --json output for automation",
		},
		RunE: jsoncontract.RunE(WorkitemPriorityJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runPriority(cmd.Context(), cmd, args[0], args[1], jsonOut)
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemPriorityJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

// parsePriorityLevel maps a user-supplied level to a ManualPriority and reports
// whether the request clears the assignment.
func parsePriorityLevel(raw string) (level priority.ManualPriority, clear bool, err error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high":
		return priority.High, false, nil
	case "medium", "med":
		return priority.Medium, false, nil
	case "low":
		return priority.Low, false, nil
	case "clear", "none":
		return priority.None, true, nil
	default:
		return priority.None, false, camperrors.NewValidation("priority",
			fmt.Sprintf("unknown priority %q (valid: high, medium, low, clear)", raw), nil)
	}
}

func runPriority(ctx context.Context, cmd *cobra.Command, selectorArg, levelArg string, jsonOut bool) error {
	level, clear, err := parsePriorityLevel(levelArg)
	if err != nil {
		return err
	}

	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	wi, err := selector.Resolve(ctx, root, selectorArg, selector.ResolveOptions{})
	if err != nil {
		return err
	}

	storePath := priority.StorePath(root)
	store, err := priority.Load(storePath)
	if err != nil {
		return camperrors.Wrap(err, "loading priority store")
	}

	if clear {
		priority.Clear(store, wi.Key)
	} else {
		priority.Set(store, wi.Key, level)
	}
	if err := priority.SaveOrDelete(storePath, store); err != nil {
		return camperrors.Wrap(err, "saving priority store")
	}

	if jsonOut {
		return emitPriorityJSON(cmd.OutOrStdout(), wi.Key, level, clear)
	}
	if clear {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "cleared priority: %s\n", wi.Key)
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "priority set: %s = %s\n", wi.Key, level)
	return err
}

func emitPriorityJSON(w io.Writer, key string, level priority.ManualPriority, cleared bool) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		SchemaVersion string    `json:"schema_version"`
		GeneratedAt   time.Time `json:"generated_at"`
		Key           string    `json:"key"`
		Priority      string    `json:"priority"`
		Cleared       bool      `json:"cleared"`
	}{
		SchemaVersion: WorkitemPriorityJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		Key:           key,
		Priority:      string(level),
		Cleared:       cleared,
	})
}
