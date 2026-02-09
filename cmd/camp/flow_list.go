package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/flow"
)

var flowRegistryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered flows from the registry",
	Long: `List all flows registered in .campaign/flows/registry.yaml.

Shows flow name, description, and tags in table format.

Examples:
  camp flow list`,
	Args: cobra.NoArgs,
	RunE: runFlowRegistryList,
}

func init() {
	flowCmd.AddCommand(flowRegistryListCmd)
}

func runFlowRegistryList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campaignRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	registry, err := flow.LoadRegistry(campaignRoot)
	if err != nil {
		return fmt.Errorf("loading flow registry: %w", err)
	}

	names := registry.List()
	if len(names) == 0 {
		fmt.Printf("No flows registered.\n\n")
		fmt.Printf("Add flows to: %s\n", flow.RegistryPath(campaignRoot))
		return nil
	}

	// Calculate column widths
	maxNameLen := 4  // "Name"
	maxDescLen := 11 // "Description"
	for _, name := range names {
		if len(name) > maxNameLen {
			maxNameLen = len(name)
		}
		f, _ := registry.Get(name)
		if len(f.Description) > maxDescLen {
			maxDescLen = len(f.Description)
		}
	}

	// Print header
	fmt.Printf("%-*s  %-*s  %s\n", maxNameLen, "Name", maxDescLen, "Description", "Tags")
	fmt.Printf("%s  %s  %s\n",
		strings.Repeat("-", maxNameLen),
		strings.Repeat("-", maxDescLen),
		strings.Repeat("-", 20))

	// Print flows
	for _, name := range names {
		f, _ := registry.Get(name)
		tags := strings.Join(f.Tags, ", ")
		if tags == "" {
			tags = "-"
		}
		fmt.Printf("%-*s  %-*s  %s\n", maxNameLen, name, maxDescLen, f.Description, tags)
	}

	fmt.Printf("\nTotal: %d flow(s)\n", len(names))
	return nil
}
