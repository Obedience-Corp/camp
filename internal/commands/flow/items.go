package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/workflow"
	"github.com/spf13/cobra"
)

func newItemsCommand() *cobra.Command {
	var (
		flowItemsAll  bool
		flowItemsJSON bool
	)

	cmd := &cobra.Command{
		Use:   "items [status]",
		Short: "List items in a status directory",
		Long: `List items in a status directory.

If no status is specified, lists items in the default status (usually 'active').
Use --all to list items in all status directories.

Examples:
  camp flow items              List items in default status
  camp flow items active       List items in active/
  camp flow items dungeon/completed  List items in dungeon/completed/
  camp flow items --all        List items in all statuses`,
		Args: jsoncontract.Args(WorkflowItemsSchemaVersion, func() bool { return flowItemsJSON }, cobra.MaximumNArgs(1)),
		RunE: jsoncontract.RunE(WorkflowItemsSchemaVersion, func() bool { return flowItemsJSON }, func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cwd, err := getCwd()
			if err != nil {
				return err
			}

			svc := workflow.NewService(cwd)
			if err := svc.LoadSchema(ctx); err != nil {
				return err
			}

			statuses := []string{}
			if flowItemsAll {
				statuses = svc.Schema().AllDirectories()
			} else if len(args) > 0 {
				statuses = []string{args[0]}
			} else {
				// Default to the schema's default status, or "active"
				status := svc.Schema().DefaultStatus
				if status == "" {
					status = "active"
				}
				statuses = []string{status}
			}

			if flowItemsJSON {
				return renderItemsJSON(cmd.OutOrStdout(), svc, ctx, statuses)
			}
			return renderItemsTable(cmd.OutOrStdout(), svc, ctx, statuses, flowItemsAll)
		}),
	}

	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkflowItemsSchemaVersion, func() bool { return flowItemsJSON }))
	cmd.Flags().BoolVarP(&flowItemsAll, "all", "a", false, "list all statuses")
	cmd.Flags().BoolVar(&flowItemsJSON, "json", false, "output as JSON")

	return cmd
}

func renderItemsJSON(w io.Writer, svc *workflow.Service, ctx context.Context, statuses []string) error {
	payload := FlowItemsPayload{
		SchemaVersion: WorkflowItemsSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Items:         make([]FlowStatusItem, 0, len(statuses)),
	}
	for _, status := range statuses {
		result, err := svc.List(ctx, status, workflow.ListOptions{})
		if err != nil {
			return err
		}
		payload.Items = append(payload.Items, FlowStatusItem{
			Status:  status,
			Entries: result.Items,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func renderItemsTable(w io.Writer, svc *workflow.Service, ctx context.Context, statuses []string, all bool) error {
	for _, status := range statuses {
		result, err := svc.List(ctx, status, workflow.ListOptions{})
		if err != nil {
			return err
		}

		if all {
			if _, err := fmt.Fprintf(w, "\n%s/ (%d items)\n", status, len(result.Items)); err != nil {
				return err
			}
		}

		for _, item := range result.Items {
			typeChar := " "
			if item.IsDir {
				typeChar = "d"
			}
			if _, err := fmt.Fprintf(w, "  %s %-30s %s\n", typeChar, item.Name, item.ModTime.Format(time.RFC3339)); err != nil {
				return err
			}
		}
	}
	return nil
}
