package main

import (
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/config"
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

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
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
		printConcept(c, 0)
	}

	// Always show how to customize concepts.
	fmt.Println()
	fmt.Printf("Customize concepts in %s. Nest workflows under the\n", ui.Accent(".campaign/campaign.yaml"))
	fmt.Printf("%s concept's %s list.\n", ui.Accent("workflow"), ui.Accent("children:"))
}

// printConcept renders a concept and recurses into its children, indenting each
// level so the parent -> sub-concept tree is visible.
func printConcept(c concept.Concept, depth int) {
	indent := strings.Repeat("  ", depth+1)

	desc := ""
	if c.Description != "" {
		desc = ui.Dim(" # " + c.Description)
	}
	fmt.Printf("%s%s %s %s%s\n",
		indent,
		ui.Accent(fmt.Sprintf("%-12s", c.Name)),
		ui.ArrowIcon(),
		ui.Value(c.Path),
		desc)

	// Top-level concepts show a metadata line; children stay compact.
	if depth == 0 {
		var meta []string
		if c.MaxDepth == nil {
			meta = append(meta, "depth: unlimited")
		} else if *c.MaxDepth == 0 {
			meta = append(meta, "depth: 0 (no drill)")
		} else {
			meta = append(meta, fmt.Sprintf("depth: %d", *c.MaxDepth))
		}
		if len(c.Ignore) > 0 {
			meta = append(meta, fmt.Sprintf("ignore: [%s]", strings.Join(c.Ignore, ", ")))
		}
		if c.HasItems {
			meta = append(meta, ui.Success("has items"))
		} else {
			meta = append(meta, "no items")
		}
		fmt.Printf("%s%s\n", indent+strings.Repeat(" ", 13), ui.Dim(strings.Join(meta, "  ")))
	}

	for _, child := range c.Children {
		printConcept(child, depth+1)
	}
}
