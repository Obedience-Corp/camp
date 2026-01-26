package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/paths"
)

var intentShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show detailed intent information",
	Long: `Display detailed information about a specific intent.

Supports partial ID matching - you can use:
  - Full ID: 20260119-153412-add-retry-logic
  - Time suffix: 153412-add-retry
  - Slug portion: add-retry

OUTPUT FORMATS:
  text (default)   Human-readable detailed view
  json             Full metadata in JSON format
  yaml             Full metadata in YAML format

Examples:
  camp intent show 20260119-153412...    Show by full ID
  camp intent show retry-logic           Show by partial match
  camp intent show retry -f json         JSON output
  camp intent show retry -f yaml         YAML output`,
	Args: cobra.ExactArgs(1),
	RunE: runIntentShow,
}

func init() {
	intentCmd.AddCommand(intentShowCmd)

	flags := intentShowCmd.Flags()
	flags.StringP("format", "f", "text", "Output format: text, json, yaml")
}

func runIntentShow(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]

	// Parse flags
	format, _ := cmd.Flags().GetString("format")

	// Validate format
	if format != "text" && format != "json" && format != "yaml" {
		return fmt.Errorf("invalid format: %s (use text, json, or yaml)", format)
	}

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())

	// Find the intent (supports partial matching)
	i, err := svc.Find(ctx, id)
	if err != nil {
		return fmt.Errorf("intent not found: %s", id)
	}

	// Format and output
	switch format {
	case "json":
		return showJSON(i)
	case "yaml":
		return showYAML(i)
	default:
		return showText(i)
	}
}

func showText(i *intent.Intent) error {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Intent: %s\n\n", i.ID))
	sb.WriteString(fmt.Sprintf("Title:    %s\n", i.Title))
	sb.WriteString(fmt.Sprintf("Type:     %s\n", i.Type))
	sb.WriteString(fmt.Sprintf("Status:   %s\n", i.Status))

	if i.Concept != "" {
		sb.WriteString(fmt.Sprintf("Concept:  %s\n", i.Concept))
	} else {
		sb.WriteString("Concept:  (none)\n")
	}

	if i.Author != "" {
		sb.WriteString(fmt.Sprintf("Author:   %s\n", i.Author))
	}

	sb.WriteString("\n")

	if i.Priority != "" {
		sb.WriteString(fmt.Sprintf("Priority: %s\n", i.Priority))
	}
	if i.Horizon != "" {
		sb.WriteString(fmt.Sprintf("Horizon:  %s\n", i.Horizon))
	}
	if len(i.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("Tags:     %s\n", strings.Join(i.Tags, ", ")))
	}

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Created:  %s\n", i.CreatedAt.Format("2006-01-02")))
	if !i.UpdatedAt.IsZero() {
		sb.WriteString(fmt.Sprintf("Updated:  %s\n", i.UpdatedAt.Format("2006-01-02 15:04:05")))
	}

	sb.WriteString("\n")

	// Dependencies
	sb.WriteString("Blocked By:\n")
	if len(i.BlockedBy) == 0 {
		sb.WriteString("  (none)\n")
	} else {
		for _, dep := range i.BlockedBy {
			sb.WriteString(fmt.Sprintf("  - %s\n", dep))
		}
	}

	sb.WriteString("\nDepends On:\n")
	if len(i.DependsOn) == 0 {
		sb.WriteString("  (none)\n")
	} else {
		for _, dep := range i.DependsOn {
			sb.WriteString(fmt.Sprintf("  - %s\n", dep))
		}
	}

	// Promotion criteria
	if i.PromotionCriteria != "" {
		sb.WriteString("\nPromotion Criteria:\n")
		sb.WriteString(fmt.Sprintf("  %s\n", i.PromotionCriteria))
	}

	// Path
	sb.WriteString(fmt.Sprintf("\nPath: %s\n", i.Path))

	// Content
	if i.Content != "" {
		sb.WriteString("\n")
		sb.WriteString("─────────────────────────────────────────────────\n")
		sb.WriteString("CONTENT\n")
		sb.WriteString("─────────────────────────────────────────────────\n\n")
		sb.WriteString(i.Content)
		if !strings.HasSuffix(i.Content, "\n") {
			sb.WriteString("\n")
		}
	}

	fmt.Print(sb.String())
	return nil
}

func showJSON(i *intent.Intent) error {
	output := map[string]interface{}{
		"id":                 i.ID,
		"title":              i.Title,
		"type":               string(i.Type),
		"status":             string(i.Status),
		"concept":            i.Concept,
		"author":             i.Author,
		"priority":           string(i.Priority),
		"horizon":            string(i.Horizon),
		"tags":               i.Tags,
		"blocked_by":         i.BlockedBy,
		"depends_on":         i.DependsOn,
		"promotion_criteria": i.PromotionCriteria,
		"promoted_to":        i.PromotedTo,
		"created_at":         i.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"path":               i.Path,
		"content":            i.Content,
	}

	if !i.UpdatedAt.IsZero() {
		output["updated_at"] = i.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

func showYAML(i *intent.Intent) error {
	// Create a clean struct for YAML output
	output := struct {
		ID                string   `yaml:"id"`
		Title             string   `yaml:"title"`
		Type              string   `yaml:"type"`
		Status            string   `yaml:"status"`
		Concept           string   `yaml:"concept,omitempty"`
		Author            string   `yaml:"author,omitempty"`
		Priority          string   `yaml:"priority,omitempty"`
		Horizon           string   `yaml:"horizon,omitempty"`
		Tags              []string `yaml:"tags,omitempty"`
		BlockedBy         []string `yaml:"blocked_by,omitempty"`
		DependsOn         []string `yaml:"depends_on,omitempty"`
		PromotionCriteria string   `yaml:"promotion_criteria,omitempty"`
		PromotedTo        string   `yaml:"promoted_to,omitempty"`
		CreatedAt         string   `yaml:"created_at"`
		UpdatedAt         string   `yaml:"updated_at,omitempty"`
		Path              string   `yaml:"path"`
		Content           string   `yaml:"content,omitempty"`
	}{
		ID:                i.ID,
		Title:             i.Title,
		Type:              string(i.Type),
		Status:            string(i.Status),
		Concept:           i.Concept,
		Author:            i.Author,
		Priority:          string(i.Priority),
		Horizon:           string(i.Horizon),
		Tags:              i.Tags,
		BlockedBy:         i.BlockedBy,
		DependsOn:         i.DependsOn,
		PromotionCriteria: i.PromotionCriteria,
		PromotedTo:        i.PromotedTo,
		CreatedAt:         i.CreatedAt.Format("2006-01-02"),
		Path:              i.Path,
		Content:           i.Content,
	}

	if !i.UpdatedAt.IsZero() {
		output.UpdatedAt = i.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
	}

	data, err := yaml.Marshal(output)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	fmt.Print(string(data))
	return nil
}
