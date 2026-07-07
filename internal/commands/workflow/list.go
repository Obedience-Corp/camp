package workflow

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
)

func newListCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List user-created workflow collections",
		Long: `List user-created workflow collections registered in the campaign.

The command reads campaign configuration and workflow/ directories, then shows
each collection's shortcut, item count, and latest workitem update. Built-in
workflow types are omitted so the output focuses on custom collections. Use
--json for machine-readable workflow inventory output.`,
		Args: jsoncontract.Args(JSONSchemaVersion, func() bool { return jsonOut }, cobra.NoArgs),
		RunE: jsoncontract.RunE(JSONSchemaVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), cmd, jsonOut)
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(JSONSchemaVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func runList(ctx context.Context, cmd *cobra.Command, jsonOut bool) error {
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	entries, err := enumerateWorkflowEntries(campaignRoot, cfg)
	if err != nil {
		return err
	}
	entries, err = populateWorkitemStats(campaignRoot, entries)
	if err != nil {
		return err
	}

	if jsonOut {
		return emitListJSON(cmd.OutOrStdout(), entries)
	}
	return emitListHuman(cmd.OutOrStdout(), entries)
}

func emitListHuman(w io.Writer, entries []workflowEntry) error {
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "no user-created workflows. create one with: camp workflow create <type> --shortcut <key>")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "TYPE\tCATEGORY\tSHORTCUT\tITEMS\tUPDATED"); err != nil {
		return err
	}
	for _, e := range entries {
		shortcut := e.ShortcutKey
		if shortcut == "" {
			shortcut = "-"
		}
		category := e.Category
		if category == "" {
			category = "-"
		}
		updated := "-"
		if !e.LastModified.IsZero() {
			updated = e.LastModified.Format(time.RFC3339)
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", e.Type, category, shortcut, e.WorkitemCount, updated); err != nil {
			return err
		}
	}
	return tw.Flush()
}

type listJSONEntry struct {
	Type          string    `json:"type"`
	Path          string    `json:"path"`
	Category      string    `json:"category,omitempty"`
	Shortcut      string    `json:"shortcut,omitempty"`
	WorkitemCount int       `json:"workitem_count"`
	HasConcept    bool      `json:"has_concept"`
	HasDir        bool      `json:"has_dir"`
	HasShortcut   bool      `json:"has_shortcut"`
	LastModified  time.Time `json:"last_modified,omitzero"`
}

func emitListJSON(w io.Writer, entries []workflowEntry) error {
	out := struct {
		SchemaVersion string          `json:"schema_version"`
		GeneratedAt   time.Time       `json:"generated_at"`
		Workflows     []listJSONEntry `json:"workflows"`
	}{
		SchemaVersion: JSONSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Workflows:     make([]listJSONEntry, 0, len(entries)),
	}
	for _, e := range entries {
		out.Workflows = append(out.Workflows, listJSONEntry{
			Type:          e.Type,
			Path:          e.Path,
			Category:      e.Category,
			Shortcut:      e.ShortcutKey,
			WorkitemCount: e.WorkitemCount,
			HasConcept:    e.HasConcept,
			HasDir:        e.HasDir,
			HasShortcut:   e.HasShortcut,
			LastModified:  e.LastModified.UTC(),
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
