package intent

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var intentListCmd = newIntentListCommand()

func newIntentListCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List ideas in the campaign",
		Long: `List intents with filtering, sorting, and output format options.

By default, lists intents in inbox, active, and ready status.
Use --all to include dungeon intents.

OUTPUT FORMATS:
  table (default)   Human-readable table with columns
  simple            IDs only, one per line (for scripting)
  json              Full metadata in JSON format

Examples:
  camp idea list                         List active intents
  camp idea ls --status inbox            List inbox only
  camp idea list -f json                 JSON output
  camp idea list -f simple | xargs ...   Pipe IDs to commands
  camp idea list --all                   Include archived`,
	}
	jsonRequested := func() bool { return intentJSONRequested(cmd, &jsonOut) }
	cmd.Args = jsoncontract.Args(IntentJSONVersion, jsonRequested, cobra.NoArgs)
	cmd.RunE = jsoncontract.RunE(IntentJSONVersion, jsonRequested, runIntentList)
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(IntentJSONVersion, jsonRequested))

	flags := cmd.Flags()
	flags.StringP("format", "f", "table", "Output format: table, simple, json")
	flags.BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	flags.StringP("sort", "S", "updated", "Sort by: updated, created, priority, title")
	flags.StringSliceP("status", "s", nil, "Filter by status (repeatable)")
	flags.StringSliceP("type", "t", nil, "Filter by type (repeatable)")
	flags.StringP("project", "p", "", "Filter by project")
	flags.String("horizon", "", "Filter by horizon")
	flags.IntP("limit", "n", 0, "Limit results (0 = no limit)")
	flags.BoolP("all", "a", false, "Include dungeon intents")
	return cmd
}

func init() {
	Cmd.AddCommand(intentListCmd)
}

func runIntentList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	format, _ := cmd.Flags().GetString("format")
	jsonOut, _ := cmd.Flags().GetBool("json")
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
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	campaignRoot, err = pathutil.ResolveRoot(campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "resolving campaign root")
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())
	warnPendingLegacyMigration(svc)

	// Build list options
	opts, err := buildListOptions(statuses, types, project, horizon, sortBy, includeAll)
	if err != nil {
		return err
	}

	// Get intents
	intents, err := svc.List(ctx, opts)
	if err != nil {
		return camperrors.Wrap(err, "failed to list intents")
	}

	// Apply status filtering (exclude dungeon statuses by default)
	intents = filterStatuses(intents, includeAll, statuses)
	intents = filterTypes(intents, types)

	// Apply limit
	if limit > 0 && len(intents) > limit {
		intents = intents[:limit]
	}

	// Format output
	switch {
	case jsonOut || format == "json":
		return outputIntentPayload(cmd.OutOrStdout(), campaignRoot, intents)
	case format == "simple":
		return outputSimple(intents)
	default:
		return outputTable(intents)
	}
}

func filterTypes(intents []*intent.Intent, allowedTypes []string) []*intent.Intent {
	if len(allowedTypes) == 0 {
		return intents
	}
	typeSet := make(map[intent.Type]bool, len(allowedTypes))
	for _, raw := range allowedTypes {
		typeSet[intent.Type(raw)] = true
	}

	result := make([]*intent.Intent, 0, len(intents))
	for _, i := range intents {
		if typeSet[i.Type] {
			result = append(result, i)
		}
	}
	return result
}

func buildListOptions(statuses, types []string, project, horizon, sortBy string, includeAll bool) (*intent.ListOptions, error) {
	opts := &intent.ListOptions{
		Concept:  project, // project from CLI maps to Concept field
		SortBy:   sortBy,
		SortDesc: sortBy == "updated" || sortBy == "created" || sortBy == "priority",
	}

	// Handle status filtering
	if len(statuses) == 1 {
		s, err := parseIntentStatus(statuses[0])
		if err != nil {
			return nil, err
		}
		opts.Status = &s
	} else if len(statuses) > 1 {
		// Service takes a single status filter; apply multi-status filters client-side.
	} else if !includeAll {
		// By default, exclude dungeon statuses (handled in list filtering later)
		// Note: service doesn't support "not in" filter, so we handle client-side
	}

	return opts, nil
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

// filterStatuses filters intents by excluding dungeon statuses unless specified.
func filterStatuses(intents []*intent.Intent, includeAll bool, allowedStatuses []string) []*intent.Intent {
	if includeAll && len(allowedStatuses) == 0 {
		return intents
	}

	normalizedAllowed := make([]intent.Status, 0, len(allowedStatuses))
	for _, raw := range allowedStatuses {
		s, err := parseIntentStatus(raw)
		if err == nil {
			normalizedAllowed = append(normalizedAllowed, s)
		}
	}

	result := make([]*intent.Intent, 0, len(intents))
	for _, i := range intents {
		// If specific statuses requested, check if matches
		if len(normalizedAllowed) > 0 {
			found := false
			for _, s := range normalizedAllowed {
				if i.Status == s {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		} else if !includeAll {
			// Exclude all dungeon statuses by default
			if i.Status.InDungeon() {
				continue
			}
		}
		result = append(result, i)
	}

	return result
}
