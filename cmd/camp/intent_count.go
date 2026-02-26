package main

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var intentCountCmd = &cobra.Command{
	Use:   "count",
	Short: "Count intents by status directory",
	Long: `Display a count of intents grouped by status directory.

OUTPUT FORMATS:
  table (default)   Styled summary with counts per status
  json              Machine-readable JSON output

Examples:
  camp intent count              Show counts per status
  camp intent count -f json      JSON output for scripting`,
	RunE: runIntentCount,
}

func init() {
	intentCmd.AddCommand(intentCountCmd)
	intentCountCmd.Flags().StringP("format", "f", "table", "Output format: table, json")
}

func runIntentCount(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	format, _ := cmd.Flags().GetString("format")

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())

	// Ensure directories exist and migrate legacy layout
	if err := svc.EnsureDirectories(ctx); err != nil {
		return fmt.Errorf("ensuring intent directories: %w", err)
	}

	counts, total, err := svc.Count(ctx)
	if err != nil {
		return fmt.Errorf("counting intents: %w", err)
	}

	switch format {
	case "json":
		return outputCountJSON(counts, total)
	default:
		return outputCountTable(counts, total)
	}
}

func outputCountTable(counts []intent.StatusCount, total int) error {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.BrightColor)
	countStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.BrightColor)
	totalStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.AccentColor)
	dimStyle := lipgloss.NewStyle().Foreground(ui.DimColor)

	fmt.Println(titleStyle.Render("Intent Counts"))
	fmt.Println()

	for _, sc := range counts {
		statusStr := ui.GetIntentStatusStyle(string(sc.Status)).
			Width(8).
			Render(string(sc.Status))

		var countStr string
		if sc.Count == 0 {
			countStr = dimStyle.Render("0")
		} else {
			countStr = countStyle.Render(fmt.Sprintf("%d", sc.Count))
		}

		fmt.Printf("  %s  %s\n", statusStr, countStr)
	}

	fmt.Println()
	fmt.Printf("  %s  %s\n",
		dimStyle.Width(8).Render("total"),
		totalStyle.Render(fmt.Sprintf("%d", total)),
	)

	return nil
}

func outputCountJSON(counts []intent.StatusCount, total int) error {
	type jsonCount struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	type jsonOutput struct {
		Counts []jsonCount `json:"counts"`
		Total  int         `json:"total"`
	}

	out := jsonOutput{
		Counts: make([]jsonCount, len(counts)),
		Total:  total,
	}
	for i, sc := range counts {
		out.Counts[i] = jsonCount{
			Status: string(sc.Status),
			Count:  sc.Count,
		}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}

	fmt.Println(string(data))
	return nil
}
