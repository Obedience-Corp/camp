package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/obediencecorp/camp/internal/config"
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
with 'camp register'. The registry lives at ~/.config/campaign/registry.yaml.

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

	listCmd.Flags().StringP("format", "f", "table", "Output format (table, simple, json)")
	listCmd.Flags().StringP("sort", "s", "accessed", "Sort by (name, accessed, type)")
}

func runList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return err
	}

	if reg.Len() == 0 {
		fmt.Println("No campaigns registered.")
		fmt.Println("Create one with: camp init")
		fmt.Println("Or register existing: camp register <path>")
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
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tPATH")
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
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", shortID, c.Name, campaignType, c.Path)
		}
		return w.Flush()
	}
}
