package main

import (
	"encoding/json"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// campaignEntry represents a campaign for display purposes.
type campaignEntry struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Path       string    `json:"path"`
	LastAccess time.Time `json:"last_access,omitempty"`
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered campaigns",
	Long: `List all campaigns registered in the global registry.

Campaigns are registered when created with 'camp init' or manually
with 'camp register'. The registry lives at ~/.obey/campaign/registry.yaml.

Output formats:
  table   - Aligned columns with headers (default)
  simple  - Campaign names only, one per line
  json    - JSON array for scripting

Sorting options:
  accessed - Most recently accessed first (default)
  name     - Alphabetically by name
  type     - Alphabetically by type

Examples:
  camp list                  List all campaigns
  camp list --format json    Output as JSON
  camp list --sort name      Sort by name
  camp list --format simple  Names only for scripting`,
	Aliases: []string{"ls"},
	RunE:    runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.GroupID = "registry"

	listCmd.Flags().StringP("format", "f", "table", "Output format (table, simple, json)")
	listCmd.Flags().StringP("sort", "s", "accessed", "Sort by (name, accessed, type)")
	listCmd.Flags().Bool("verify-verbose", false, "Show detailed verification output")
}

func runList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return err
	}

	// Verify and self-heal registry
	report, err := reg.VerifyAndRepair(ctx)
	if err != nil {
		return camperrors.Wrap(err, "registry verification failed")
	}

	// Save if changes made
	if report.HasChanges() {
		if err := config.SaveRegistry(ctx, reg); err != nil {
			return camperrors.Wrap(err, "failed to save registry")
		}

		verbose, _ := cmd.Flags().GetBool("verify-verbose")
		if verbose {
			printVerificationDetails(report)
		} else {
			printVerificationSummary(report)
		}
	}

	if reg.Len() == 0 {
		fmt.Println(ui.Warning("No campaigns registered."))
		fmt.Println()
		fmt.Printf("  Create one with: %s\n", ui.Accent("camp init"))
		fmt.Printf("  Or register existing: %s\n", ui.Accent("camp register <path>"))
		return nil
	}

	formatStr, _ := cmd.Flags().GetString("format")
	sortBy, _ := cmd.Flags().GetString("sort")

	campaigns := sortCampaigns(reg.Campaigns, sortBy)
	return outputCampaigns(campaigns, formatStr)
}

// sortCampaigns converts the registry map to a sorted slice.
func sortCampaigns(campaigns map[string]config.RegisteredCampaign, by string) []campaignEntry {
	entries := make([]campaignEntry, 0, len(campaigns))
	for id, c := range campaigns {
		entries = append(entries, campaignEntry{
			ID:         id,
			Name:       c.Name,
			Type:       string(c.Type),
			Path:       c.Path,
			LastAccess: c.LastAccess,
		})
	}

	switch by {
	case "name":
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name < entries[j].Name
		})
	case "type":
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Type == entries[j].Type {
				return entries[i].Name < entries[j].Name
			}
			return entries[i].Type < entries[j].Type
		})
	default: // "accessed"
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].LastAccess.After(entries[j].LastAccess)
		})
	}

	return entries
}

// outputCampaigns writes campaigns to stdout in the specified format.
func outputCampaigns(campaigns []campaignEntry, format string) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(campaigns)
	case "simple":
		for _, c := range campaigns {
			fmt.Println(c.Name)
		}
		return nil
	default: // table
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			ui.Label("ID"), ui.Label("NAME"), ui.Label("TYPE"), ui.Label("PATH"))
		for _, c := range campaigns {
			campaignType := c.Type
			if campaignType == "" {
				campaignType = "-"
			}
			// Truncate ID for display (first 8 chars like git)
			shortID := c.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			typeStyle := ui.GetCampaignTypeStyle(c.Type)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				ui.Dim(shortID),
				ui.Value(c.Name),
				typeStyle.Render(campaignType),
				ui.Dim(c.Path))
		}
		return w.Flush()
	}
}

// printVerificationSummary prints a brief summary of verification changes.
func printVerificationSummary(r *config.VerificationReport) {
	var parts []string
	if len(r.Removed) > 0 {
		parts = append(parts, fmt.Sprintf("removed %d", len(r.Removed)))
	}
	if len(r.Added) > 0 {
		parts = append(parts, fmt.Sprintf("added %d", len(r.Added)))
	}
	if len(r.Updated) > 0 {
		parts = append(parts, fmt.Sprintf("updated %d", len(r.Updated)))
	}
	fmt.Printf("%s Registry cleaned: %s\n\n", ui.SuccessIcon(), strings.Join(parts, ", "))
}

// printVerificationDetails prints detailed information about verification changes.
func printVerificationDetails(r *config.VerificationReport) {
	fmt.Println("Registry verification:")
	for _, e := range r.Removed {
		fmt.Printf("  %s removed: %s (%s) - %s\n", ui.WarningIcon(), e.Name, e.Path, e.Reason)
	}
	for _, e := range r.Added {
		fmt.Printf("  %s added: %s (%s)\n", ui.SuccessIcon(), e.Name, e.Path)
	}
	for _, e := range r.Updated {
		fmt.Printf("  %s updated: %s - %s\n", ui.InfoIcon(), e.Path, strings.Join(e.Changes, ", "))
	}
	fmt.Println()
}
