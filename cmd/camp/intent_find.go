package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/paths"
	"github.com/obediencecorp/camp/internal/ui"
)

var intentFindCmd = &cobra.Command{
	Use:   "find [query]",
	Short: "Search for intents by title or content",
	Long: `Search for intents across all statuses by title, content, or ID.

The search is case-insensitive and matches partial strings.
Without a query, returns all intents.

OUTPUT FORMATS:
  table (default)   Human-readable table with columns
  simple            IDs only, one per line (for scripting)
  json              Full metadata in JSON format

Examples:
  camp intent find                   List all intents
  camp intent find dark              Find intents containing "dark"
  camp intent find "bug fix"         Find intents with "bug fix"
  camp intent find -f simple auth    Get IDs of auth-related intents`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIntentFind,
}

func init() {
	intentCmd.AddCommand(intentFindCmd)

	flags := intentFindCmd.Flags()
	flags.StringP("format", "f", "table", "Output format: table, simple, json")
	flags.IntP("limit", "n", 0, "Limit results (0 = no limit)")
}

func runIntentFind(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	format, _ := cmd.Flags().GetString("format")
	limit, _ := cmd.Flags().GetInt("limit")

	// Get query (optional)
	query := ""
	if len(args) > 0 {
		query = args[0]
	}

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())

	// Search for intents
	intents, err := svc.Search(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to search intents: %w", err)
	}

	// Apply limit
	if limit > 0 && len(intents) > limit {
		intents = intents[:limit]
	}

	// Format output
	switch format {
	case "json":
		return outputFindJSON(intents)
	case "simple":
		return outputFindSimple(intents)
	default:
		return outputFindTable(intents, query)
	}
}

func outputFindTable(intents []*intent.Intent, query string) error {
	if len(intents) == 0 {
		if query != "" {
			fmt.Printf("No intents found matching %q\n", query)
		} else {
			fmt.Println("No intents found.")
		}
		return nil
	}

	// Define styles
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)

	// Status color function
	statusColor := func(s intent.Status) string {
		return ui.GetIntentStatusStyle(string(s)).Render(string(s))
	}

	// Build table data
	headers := []string{"ID", "TITLE", "TYPE", "STATUS", "UPDATED"}
	rows := make([][]string, 0, len(intents))

	for _, i := range intents {
		id := truncate(i.ID, 25)
		title := truncate(i.Title, 40)
		updated := relativeTime(i.UpdatedAt)
		if i.UpdatedAt.IsZero() {
			updated = relativeTime(i.CreatedAt)
		}

		rows = append(rows, []string{
			id,
			title,
			string(i.Type),
			statusColor(i.Status),
			updated,
		})
	}

	// Create table
	t := table.New().
		Border(lipgloss.HiddenBorder()).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return lipgloss.NewStyle()
		})

	fmt.Println(t)
	if query != "" {
		fmt.Printf("\n%d result(s) for %q\n", len(intents), query)
	} else {
		fmt.Printf("\n%d intent(s)\n", len(intents))
	}
	return nil
}

func outputFindSimple(intents []*intent.Intent) error {
	for _, i := range intents {
		fmt.Println(i.ID)
	}
	return nil
}

func outputFindJSON(intents []*intent.Intent) error {
	type jsonIntent struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Type      string `json:"type"`
		Status    string `json:"status"`
		Project   string `json:"project,omitempty"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at,omitempty"`
		Path      string `json:"path"`
	}

	output := make([]jsonIntent, 0, len(intents))
	for _, i := range intents {
		j := jsonIntent{
			ID:        i.ID,
			Title:     i.Title,
			Type:      string(i.Type),
			Status:    string(i.Status),
			Project:   i.Project,
			CreatedAt: i.CreatedAt.Format(time.RFC3339),
			Path:      i.Path,
		}
		if !i.UpdatedAt.IsZero() {
			j.UpdatedAt = i.UpdatedAt.Format(time.RFC3339)
		}
		output = append(output, j)
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Println(string(data))
	return nil
}
