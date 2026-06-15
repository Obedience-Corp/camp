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
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	Description string   `json:"description,omitempty"`
	MaxDepth    *int     `json:"max_depth,omitempty"`
	HasItems    bool     `json:"has_items"`
	Ignore      []string `json:"ignore,omitempty"`
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
		items = append(items, conceptItem{
			Name:        c.Name,
			Path:        c.Path,
			Description: c.Description,
			MaxDepth:    c.MaxDepth,
			HasItems:    c.HasItems,
			Ignore:      c.Ignore,
		})
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
