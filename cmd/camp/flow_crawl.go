package main

import (
	"context"
	"fmt"

	"github.com/obediencecorp/camp/internal/ui"
	"github.com/obediencecorp/camp/internal/workflow"
	"github.com/spf13/cobra"
)

var flowCrawlAll bool

var flowCrawlCmd = &cobra.Command{
	Use:   "crawl [status]",
	Short: "Interactive item review",
	Long: `Interactively review and move items across workflow statuses.

If no status is specified, crawls all statuses in order.
For each item, you can choose to keep, move to another status, or skip.

Examples:
  camp flow crawl              Review all statuses
  camp flow crawl active       Review items in active/ only`,
	Args: cobra.MaximumNArgs(1),
	RunE: runFlowCrawl,
}

func init() {
	flowCmd.AddCommand(flowCrawlCmd)
	flowCrawlCmd.Flags().BoolVarP(&flowCrawlAll, "all", "a", false, "include recently modified items")
}

func runFlowCrawl(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cwd, err := getCwd()
	if err != nil {
		return err
	}

	svc := workflow.NewService(cwd)
	if err := svc.LoadSchema(ctx); err != nil {
		return err
	}

	status := ""
	if len(args) > 0 {
		status = args[0]
	}

	opts := workflow.CrawlOptions{
		Status: status,
		All:    flowCrawlAll,
	}

	result, err := svc.Crawl(ctx, opts)
	if err != nil {
		return err
	}

	// Display summary
	fmt.Println()
	ui.Success("Crawl complete!")
	fmt.Printf("  Reviewed: %d\n", result.Reviewed)
	fmt.Printf("  Kept: %d\n", result.Kept)
	fmt.Printf("  Moved: %d\n", result.Moved)
	fmt.Printf("  Skipped: %d\n", result.Skipped)

	return nil
}
