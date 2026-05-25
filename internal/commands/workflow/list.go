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
)

func newListCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List user-created workflow collections",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), cmd, jsonOut)
		},
	}
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
		fmt.Fprintln(w, "no user-created workflows. create one with: camp workflow create <type> --shortcut <key>")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TYPE\tSHORTCUT\tITEMS\tUPDATED")
	for _, e := range entries {
		shortcut := e.ShortcutKey
		if shortcut == "" {
			shortcut = "-"
		}
		updated := "-"
		if !e.LastModified.IsZero() {
			updated = e.LastModified.Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", e.Type, shortcut, e.WorkitemCount, updated)
	}
	return tw.Flush()
}

type listJSONEntry struct {
	Type          string    `json:"type"`
	Path          string    `json:"path"`
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
