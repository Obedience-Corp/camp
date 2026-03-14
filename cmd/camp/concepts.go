package main

import (
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var conceptsCmd = &cobra.Command{
	Use:     "concepts",
	Short:   "List configured concepts",
	Aliases: []string{"concept"},
	RunE:    runConcepts,
}

func init() {
	rootCmd.AddCommand(conceptsCmd)
	conceptsCmd.GroupID = "campaign"
}

func runConcepts(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	cfg, campaignRoot, err := cmdutil.LoadCampaignConfigSafe(ctx)
	if err != nil {
		fmt.Println(ui.Warning("Not in a campaign"))
		fmt.Println()
		fmt.Printf("Run %s to create a new campaign.\n", ui.Accent("camp init"))
		return nil
	}

	svc := concept.NewService(campaignRoot, cfg.Concepts())
	concepts, err := svc.List(ctx)
	if err != nil {
		return err
	}

	printConcepts(cfg.Name, concepts)
	return nil
}

func printConcepts(campaignName string, concepts []concept.Concept) {
	fmt.Println(ui.Subheader("Concepts"))
	fmt.Printf("Campaign: %s\n", ui.Accent(campaignName))
	fmt.Println()

	if len(concepts) == 0 {
		fmt.Println(ui.Dim("No concepts configured."))
		fmt.Println()
		fmt.Printf("Add concepts in %s:\n", ui.Accent(".campaign/campaign.yaml"))
		fmt.Println()
		fmt.Println(ui.Dim("  concepts:"))
		fmt.Println(ui.Dim("    - name: p"))
		fmt.Println(ui.Dim("      path: \"projects/\""))
		fmt.Println(ui.Dim("      description: \"Project directories\""))
		return
	}

	for _, c := range concepts {
		desc := ""
		if c.Description != "" {
			desc = ui.Dim(" # " + c.Description)
		}
		fmt.Printf("  %s %s %s%s\n",
			ui.Accent(fmt.Sprintf("%-8s", c.Name)),
			ui.ArrowIcon(),
			ui.Value(c.Path),
			desc)

		// Metadata line
		var meta []string

		// Depth info
		if c.MaxDepth == nil {
			meta = append(meta, "depth: unlimited")
		} else if *c.MaxDepth == 0 {
			meta = append(meta, "depth: 0 (no drill)")
		} else {
			meta = append(meta, fmt.Sprintf("depth: %d", *c.MaxDepth))
		}

		// Ignore patterns
		if len(c.Ignore) > 0 {
			meta = append(meta, fmt.Sprintf("ignore: [%s]", strings.Join(c.Ignore, ", ")))
		}

		// Items indicator
		if c.HasItems {
			meta = append(meta, ui.Success("has items"))
		} else {
			meta = append(meta, "no items")
		}

		fmt.Printf("  %s %s\n", strings.Repeat(" ", 8), ui.Dim(strings.Join(meta, "  ")))
	}
}
