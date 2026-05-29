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
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
)

func newResolveCommand() *cobra.Command {
	var (
		explicit, festival string
		jsonOut, explain   bool
	)
	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "Print the workitem the current context resolves to (read-only)",
		Args:  jsoncontract.Args(WorkitemResolveJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only context resolution with --json output for automation",
		},
		RunE: jsoncontract.RunE(WorkitemResolveJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runResolve(cmd.Context(), cmd, resolveOptions{
				Explicit:   explicit,
				FestivalID: festival,
				JSON:       jsonOut,
				Explain:    explain,
			})
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemResolveJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().StringVar(&explicit, "workitem", "", "explicit workitem selector (overrides cwd-based detection)")
	cmd.Flags().StringVar(&festival, "festival", "", "festival id for the festival tier")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	cmd.Flags().BoolVar(&explain, "explain", false, "print the tier-by-tier resolution trace")
	return cmd
}

type resolveOptions struct {
	Explicit   string
	FestivalID string
	JSON       bool
	Explain    bool
}

func runResolve(ctx context.Context, cmd *cobra.Command, opts resolveOptions) error {
	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	res, err := resolver.Resolve(ctx, root, resolver.Options{
		Explicit:   opts.Explicit,
		FestivalID: opts.FestivalID,
	})
	if err != nil {
		return err
	}
	if opts.JSON {
		return emitResolveJSON(cmd.OutOrStdout(), res)
	}
	return emitResolveHuman(cmd.OutOrStdout(), res, opts.Explain)
}

func emitResolveHuman(w io.Writer, res *resolver.Resolution, explain bool) error {
	if explain {
		for _, step := range res.Trace {
			detail := step.Detail
			if detail != "" {
				detail = " — " + detail
			}
			if _, err := fmt.Fprintf(w, "  [%s] %s%s\n", step.Tier, step.Result, detail); err != nil {
				return err
			}
		}
	}
	if res.Workitem == nil {
		_, err := fmt.Fprintln(w, "no workitem context")
		return err
	}
	quest := "-"
	if res.QuestID != "" {
		quest = res.QuestID
	}
	_, err := fmt.Fprintf(w,
		"workitem: %s (source: %s, quest: %s)\n",
		identityOf(res.Workitem), res.Source, quest)
	return err
}

func emitResolveJSON(w io.Writer, res *resolver.Resolution) error {
	if res.Trace == nil {
		res.Trace = []resolver.TraceStep{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		SchemaVersion string               `json:"schema_version"`
		GeneratedAt   time.Time            `json:"generated_at"`
		Resolution    *resolver.Resolution `json:"resolution"`
	}{
		SchemaVersion: WorkitemResolveJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		Resolution:    res,
	})
}

func identityOf(wi *wkitem.WorkItem) string {
	if wi == nil {
		return ""
	}
	if wi.StableID != "" {
		return wi.StableID
	}
	if wi.Key != "" {
		return wi.Key
	}
	return strings.TrimSpace(wi.Title)
}
