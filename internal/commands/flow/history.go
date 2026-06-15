package flow

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/workflow"
)

func newHistoryCommand() *cobra.Command {
	var (
		limit int
		item  string
	)

	cmd := &cobra.Command{
		Use:   "history",
		Short: "View workflow transition history",
		Long: `View workflow transition history recorded in the workflow history file.

The command reads .workflow-history.jsonl, or the history_file configured in
.workflow.yaml, and prints recorded item moves. Use --item to filter to one
item and --limit to show only the most recent entries.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := getCwd()
			if err != nil {
				return err
			}

			svc := workflow.NewService(cwd)
			entries, err := svc.History(cmd.Context(), workflow.HistoryOptions{
				Limit: limit,
				Item:  item,
			})
			if err != nil {
				return err
			}
			return printHistory(cmd, entries)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum entries to show (0 for all)")
	cmd.Flags().StringVar(&item, "item", "", "filter to one item name")
	return cmd
}

func printHistory(cmd *cobra.Command, entries []workflow.HistoryEntry) error {
	w := cmd.OutOrStdout()
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "no history")
		return err
	}
	for _, entry := range entries {
		timestamp := "-"
		if !entry.Timestamp.IsZero() {
			timestamp = entry.Timestamp.Format(time.RFC3339)
		}
		if entry.Reason != "" {
			if _, err := fmt.Fprintf(w, "%s  %s: %s -> %s (%s)\n", timestamp, entry.Item, entry.From, entry.To, entry.Reason); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "%s  %s: %s -> %s\n", timestamp, entry.Item, entry.From, entry.To); err != nil {
			return err
		}
	}
	return nil
}
