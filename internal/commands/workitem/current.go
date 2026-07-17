package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

func newCurrentCommand() *cobra.Command {
	var clear, jsonOut bool
	cmd := &cobra.Command{
		Use:   "current [selector]",
		Short: "Get, set, or clear the current workitem",
		Long: `Get, set, or clear the campaign-local current workitem pointer.

The selection is stored in .campaign/workitems/current.yaml and is used by
commands that need a default workitem when cwd alone is ambiguous. Pass a
selector to set the current workitem, omit it to read the selection, or use
--clear to remove it. Use --json for machine-readable current selection output.`,
		Args: jsoncontract.Args(links.CurrentSchemaVersion, func() bool { return jsonOut }, cobra.RangeArgs(0, 1)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Gets or sets current workitem with --json output for automation",
		},
		RunE: jsoncontract.RunE(links.CurrentSchemaVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			selectorArg := ""
			if len(args) == 1 {
				selectorArg = args[0]
			}
			return runCurrent(cmd.Context(), cmd, selectorArg, clear, jsonOut)
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(links.CurrentSchemaVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the local current.yaml selection")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func runCurrent(ctx context.Context, cmd *cobra.Command, selectorArg string, clear, jsonOut bool) error {
	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	switch {
	case clear:
		if selectorArg != "" {
			return camperrors.NewValidation("clear", "--clear and a selector are mutually exclusive", nil)
		}
		if err := links.SaveCurrent(ctx, root, nil); err != nil {
			return err
		}
		if jsonOut {
			return emitCurrentJSON(cmd.OutOrStdout(), nil, true)
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "cleared current workitem")
		return err

	case selectorArg != "":
		wi, err := resolveSelector(ctx, root, selectorArg, false)
		if err != nil {
			return err
		}
		cur := &links.Current{
			Version:    links.CurrentSchemaVersion,
			WorkitemID: wi.StableID,
			SelectedAt: time.Now().UTC().Truncate(time.Second),
		}
		if wi.StableID == "" {
			cur.WorkitemID = wi.Key
		}
		if err := links.SaveCurrent(ctx, root, cur); err != nil {
			return err
		}
		if jsonOut {
			return emitCurrentJSON(cmd.OutOrStdout(), cur, false)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "set current workitem: %s\n", cur.WorkitemID)
		return err

	default:
		cur, err := links.LoadCurrent(ctx, root)
		if err != nil {
			return err
		}
		if jsonOut {
			return emitCurrentJSON(cmd.OutOrStdout(), cur, false)
		}
		if cur == nil {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "no current workitem")
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(),
			"current workitem: %s (selected %s)\n",
			cur.WorkitemID, cur.SelectedAt.Format(time.RFC3339))
		return err
	}
}

func emitCurrentJSON(w io.Writer, cur *links.Current, cleared bool) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		SchemaVersion string         `json:"schema_version"`
		GeneratedAt   time.Time      `json:"generated_at"`
		Cleared       bool           `json:"cleared"`
		Current       *links.Current `json:"current"`
	}{
		SchemaVersion: links.CurrentSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Cleared:       cleared,
		Current:       cur,
	})
}
