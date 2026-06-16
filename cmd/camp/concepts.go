package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

const ConceptsJSONVersion = "concepts/v1alpha1"

var conceptsCmd = newConceptsCommand()

type conceptsPayload struct {
	SchemaVersion string        `json:"schema_version"`
	GeneratedAt   time.Time     `json:"generated_at"`
	CampaignRoot  string        `json:"campaign_root"`
	Concepts      []conceptItem `json:"concepts"`
}

type conceptItem struct {
	Name        string        `json:"name"`
	Path        string        `json:"path"`
	Description string        `json:"description,omitempty"`
	MaxDepth    *int          `json:"max_depth,omitempty"`
	HasItems    bool          `json:"has_items"`
	Ignore      []string      `json:"ignore,omitempty"`
	Children    []conceptItem `json:"children,omitempty"`
}

func init() {
	rootCmd.AddCommand(conceptsCmd)
	conceptsCmd.GroupID = "campaign"
}

func newConceptsCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "concepts",
		Short:   "List configured concepts",
		Aliases: []string{"concept"},
		RunE:    jsoncontract.RunE(ConceptsJSONVersion, func() bool { return jsonOut }, runConcepts),
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func runConcepts(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return jsoncontract.WithHint(
			camperrors.Wrap(err, "not in a campaign directory"),
			"run 'camp init' to create a new campaign",
		)
	}

	svc := concept.NewService(campaignRoot, cfg.Concepts())
	concepts, err := svc.List(ctx)
	if err != nil {
		return err
	}

	jsonOut, _ := cmd.Flags().GetBool("json")
	if jsonOut {
		return printConceptsJSON(cmd, campaignRoot, concepts)
	}

	printConcepts(cfg.Name, concepts)
	return nil
}

func printConceptsJSON(cmd *cobra.Command, campaignRoot string, concepts []concept.Concept) error {
	resolvedRoot, err := filepath.EvalSymlinks(campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "resolve campaign root")
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return camperrors.Wrap(err, "resolve campaign root")
	}

	items := make([]conceptItem, 0, len(concepts))
	for _, c := range concepts {
		items = append(items, conceptItemFromConcept(c))
	}

	payload := conceptsPayload{
		SchemaVersion: ConceptsJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		CampaignRoot:  resolvedRoot,
		Concepts:      items,
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func conceptItemFromConcept(c concept.Concept) conceptItem {
	item := conceptItem{
		Name:        c.Name,
		Path:        c.Path,
		Description: c.Description,
		MaxDepth:    c.MaxDepth,
		HasItems:    c.HasItems,
		Ignore:      c.Ignore,
	}
	if len(c.Children) > 0 {
		item.Children = make([]conceptItem, 0, len(c.Children))
		for _, child := range c.Children {
			item.Children = append(item.Children, conceptItemFromConcept(child))
		}
	}
	return item
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
