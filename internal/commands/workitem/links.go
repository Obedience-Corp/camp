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
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

func newLinksCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "links [selector]",
		Short: "List workitem links",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selectorArg := ""
			if len(args) == 1 {
				selectorArg = args[0]
			}
			return runLinks(cmd.Context(), cmd, selectorArg, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func runLinks(ctx context.Context, cmd *cobra.Command, selectorArg string, jsonOut bool) error {
	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	registry, err := links.Load(ctx, root)
	if err != nil {
		return err
	}

	registry.Sort()
	filtered := registry.Links
	if selectorArg != "" {
		wi, err := resolveSelector(ctx, root, selectorArg, false)
		if err != nil {
			return err
		}
		var matched []links.Link
		for _, link := range registry.Links {
			if link.WorkitemID == wi.StableID {
				matched = append(matched, link)
			}
		}
		filtered = matched
	}

	if jsonOut {
		return emitLinksJSON(cmd.OutOrStdout(), filtered)
	}
	return emitLinksHuman(cmd.OutOrStdout(), filtered)
}

func emitLinksHuman(w io.Writer, list []links.Link) error {
	if len(list) == 0 {
		_, err := fmt.Fprintln(w, "no links")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "LINK_ID\tWORKITEM\tSCOPE\tROLE\tCREATED")
	for _, link := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s:%s\t%s\t%s\n",
			link.ID, link.WorkitemID, link.Scope.Kind, link.Scope.Path,
			link.Role, link.CreatedAt.Format(time.RFC3339))
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
