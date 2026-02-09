package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/paths"
	"github.com/obediencecorp/camp/internal/ui"
)

var intentListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List intents in the campaign",
	Long: `List intents with filtering, sorting, and output format options.

By default, lists intents in inbox, active, and ready status.
Use --all to include done and killed intents.

OUTPUT FORMATS:
  table (default)   Human-readable table with columns
  simple            IDs only, one per line (for scripting)
  json              Full metadata in JSON format

Examples:
  camp intent list                         List active intents
  camp intent ls --status inbox            List inbox only
  camp intent list -f json                 JSON output
  camp intent list -f simple | xargs ...   Pipe IDs to commands
  camp intent list --all                   Include archived`,
	RunE: runIntentList,
}

func init() {
	intentCmd.AddCommand(intentListCmd)

	flags := intentListCmd.Flags()
	flags.StringP("format", "f", "table", "Output format: table, simple, json")
	flags.StringP("sort", "S", "updated", "Sort by: updated, created, priority, title")
	flags.StringSliceP("status", "s", nil, "Filter by status (repeatable)")
	flags.StringSliceP("type", "t", nil, "Filter by type (repeatable)")
	flags.StringP("project", "p", "", "Filter by project")
	flags.String("horizon", "", "Filter by horizon")
	flags.IntP("limit", "n", 0, "Limit results (0 = no limit)")
	flags.BoolP("all", "a", false, "Include done/killed intents")
}

func runIntentList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	format, _ := cmd.Flags().GetString("format")
	sortBy, _ := cmd.Flags().GetString("sort")
	statuses, _ := cmd.Flags().GetStringSlice("status")
	types, _ := cmd.Flags().GetStringSlice("type")
	project, _ := cmd.Flags().GetString("project")
	horizon, _ := cmd.Flags().GetString("horizon")
	limit, _ := cmd.Flags().GetInt("limit")
	includeAll, _ := cmd.Flags().GetBool("all")

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())

	// Ensure directories exist and migrate legacy layout
	if err := svc.EnsureDirectories(ctx); err != nil {
		return fmt.Errorf("ensuring intent directories: %w", err)
	}

	// Build list options
	opts := buildListOptions(statuses, types, project, horizon, sortBy, includeAll)

	// Get intents
	intents, err := svc.List(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to list intents: %w", err)
	}

	// Apply status filtering (exclude done/killed by default)
	intents = filterStatuses(intents, includeAll, statuses)

	// Apply limit
	if limit > 0 && len(intents) > limit {
		intents = intents[:limit]
	}

	// Format output
	switch format {
	case "json":
		return outputJSON(intents)
	case "simple":
		return outputSimple(intents)
	default:
		return outputTable(intents)
	}
}

func buildListOptions(statuses, types []string, project, horizon, sortBy string, includeAll bool) *intent.ListOptions {
	opts := &intent.ListOptions{
		Concept:  project, // project from CLI maps to Concept field
		SortBy:   sortBy,
		SortDesc: sortBy == "updated" || sortBy == "created" || sortBy == "priority",
	}

	// Handle status filtering
	if len(statuses) > 0 {
		// Use first status for now (service takes single status)
		s := intent.Status(statuses[0])
		opts.Status = &s
	} else if !includeAll {
		// By default, exclude done and killed (handled in list filtering later)
		// Note: service doesn't support "not in" filter, so we handle client-side
	}

	// Handle type filtering
	if len(types) > 0 {
		t := intent.Type(types[0])
		opts.Type = &t
	}

	return opts
}

func outputTable(intents []*intent.Intent) error {
	if len(intents) == 0 {
		fmt.Println("No intents found.")
		return nil
	}

	// Define styles using the central palette
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)

	// Style functions using the UI palette
	statusStyle := func(s intent.Status) string {
		return ui.GetIntentStatusStyle(string(s)).Render(string(s))
	}
	typeStyle := func(t intent.Type) string {
		return ui.GetIntentTypeStyle(string(t)).Render(string(t))
	}
	conceptStyle := func(c string) string {
		if c == "" {
			c = "-"
		}
		return ui.GetConceptStyle(c).Render(c)
	}

	// Build table data
	headers := []string{"TITLE", "TYPE", "STATUS", "CONCEPT", "UPDATED"}
	rows := make([][]string, 0, len(intents))

	for _, i := range intents {
		title := truncate(i.Title, 40)
		updated := formatTimestamp(i.UpdatedAt)
		if i.UpdatedAt.IsZero() {
			updated = formatTimestamp(i.CreatedAt)
		}

		rows = append(rows, []string{
			title,
			typeStyle(i.Type),
			statusStyle(i.Status),
			conceptStyle(i.Concept),
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
	fmt.Printf("\n%d intent(s)\n", len(intents))
	return nil
}

func outputSimple(intents []*intent.Intent) error {
	for _, i := range intents {
		fmt.Println(i.ID)
	}
	return nil
}

func outputJSON(intents []*intent.Intent) error {
	// Create JSON-friendly structure
	type jsonIntent struct {
		ID                string   `json:"id"`
		Title             string   `json:"title"`
		Type              string   `json:"type"`
		Status            string   `json:"status"`
		Concept           string   `json:"concept,omitempty"`
		Author            string   `json:"author,omitempty"`
		Priority          string   `json:"priority,omitempty"`
		Horizon           string   `json:"horizon,omitempty"`
		Tags              []string `json:"tags,omitempty"`
		BlockedBy         []string `json:"blocked_by,omitempty"`
		DependsOn         []string `json:"depends_on,omitempty"`
		PromotionCriteria string   `json:"promotion_criteria,omitempty"`
		PromotedTo        string   `json:"promoted_to,omitempty"`
		CreatedAt         string   `json:"created_at"`
		UpdatedAt         string   `json:"updated_at,omitempty"`
		Path              string   `json:"path"`
	}

	output := make([]jsonIntent, 0, len(intents))
	for _, i := range intents {
		j := jsonIntent{
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
			CreatedAt:         i.CreatedAt.Format(time.RFC3339),
			Path:              i.Path,
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

// truncate shortens a string to max length with ellipsis.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

// formatTimestamp returns a compact timestamp: "Jan 29 14:30"
func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("Jan 02 15:04")
}

// relativeTime returns a human-readable relative time string.
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}

	diff := time.Since(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		m := int(diff.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case diff < 24*time.Hour:
		h := int(diff.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	case diff < 7*24*time.Hour:
		d := int(diff.Hours() / 24)
		if d == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", d)
	case diff < 30*24*time.Hour:
		w := int(diff.Hours() / 24 / 7)
		if w == 1 {
			return "1w ago"
		}
		return fmt.Sprintf("%dw ago", w)
	default:
		m := int(diff.Hours() / 24 / 30)
		if m == 1 {
			return "1mo ago"
		}
		return fmt.Sprintf("%dmo ago", m)
	}
}

// filterStatuses filters intents by excluding done/killed unless specified.
func filterStatuses(intents []*intent.Intent, includeAll bool, allowedStatuses []string) []*intent.Intent {
	if includeAll && len(allowedStatuses) == 0 {
		return intents
	}

	result := make([]*intent.Intent, 0, len(intents))
	for _, i := range intents {
		// If specific statuses requested, check if matches
		if len(allowedStatuses) > 0 {
			found := false
			for _, s := range allowedStatuses {
				if strings.EqualFold(string(i.Status), s) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		} else if !includeAll {
			// Exclude done and killed by default
			if i.Status == intent.StatusDone || i.Status == intent.StatusKilled {
				continue
			}
		}
		result = append(result, i)
	}

	return result
}
