package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

func newLinksCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "links [selector]",
		Short: "List workitem links",
		Long: `List workitem links recorded in the camp link registry.

The command reads .campaign/workitems/links.yaml and prints every link, or only
links for the supplied workitem selector. Use this to audit which projects,
festivals, worktrees, or paths are attached to a workitem. Use --json for
machine-readable link lists.`,
		Args: jsoncontract.Args(links.LinksSchemaVersion, func() bool { return jsonOut }, cobra.RangeArgs(0, 1)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only link listing with --json output for automation",
		},
		RunE: jsoncontract.RunE(links.LinksSchemaVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			selectorArg := ""
			if len(args) == 1 {
				selectorArg = args[0]
			}
			return runLinks(cmd.Context(), cmd, selectorArg, jsonOut)
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(links.LinksSchemaVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func runLinks(ctx context.Context, cmd *cobra.Command, selectorArg string, jsonOut bool) error {
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp directory")
	}

	registry, err := links.Load(ctx, root)
	if err != nil {
		return err
	}

	// Fill the owning project on worktree rows that predate the field, so the
	// listing reads the same whether a row was written before or after it. This
	// command never saves, so the derivation stays in memory; `camp workitem
	// doctor --fix` is what writes it back.
	layout := scopeLayout(cfg)
	links.BackfillProjects(layout, registry)

	registry.Sort()
	filtered := registry.Links
	if selectorArg != "" {
		wi, err := resolveSelector(ctx, root, selectorArg, false)
		if err != nil {
			return err
		}
		var matched []links.Link
		for _, link := range registry.Links {
			if wkitem.LinkMatchesWorkitem(wi, link.WorkitemID, link.WorkitemKey) {
				matched = append(matched, link)
			}
		}
		filtered = matched
	}

	if jsonOut {
		return emitLinksJSON(cmd.OutOrStdout(), filtered)
	}
	return emitLinksHuman(cmd.OutOrStdout(), layout, filtered)
}

// emitLinksHuman prints the listing with the project as its own column. A
// worktree is a checkout of a project rather than a project of its own, so the
// project is what the row is about and the worktree path is the detail beside
// it.
func emitLinksHuman(w io.Writer, layout links.ScopeLayout, list []links.Link) error {
	if len(list) == 0 {
		_, err := fmt.Fprintln(w, "no links")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "LINK_ID\tWORKITEM\tPROJECT\tSCOPE\tROLE\tCREATED"); err != nil {
		return err
	}
	for _, link := range list {
		project := link.Scope.ProjectFor(layout)
		if project == "" {
			project = "-"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s:%s\t%s\t%s\n",
			link.ID, link.WorkitemID, project, link.Scope.Kind, link.Scope.Path,
			link.Role, link.CreatedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func emitLinksJSON(w io.Writer, list []links.Link) error {
	if list == nil {
		list = []links.Link{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		SchemaVersion string       `json:"schema_version"`
		GeneratedAt   time.Time    `json:"generated_at"`
		Links         []links.Link `json:"links"`
	}{
		SchemaVersion: links.LinksSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Links:         list,
	})
}
