package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

type campaignEntry struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Path       string    `json:"path"`
	LastAccess time.Time `json:"last_access,omitempty"`
	Org        string    `json:"org"`
	Status     string    `json:"status"`
	Tags       []string  `json:"tags"`
	// Machine is the source machine id under --remote ("local" for this machine,
	// the machine id for a remote row). omitempty keeps default `camp list --json`
	// byte-identical for every existing consumer when --remote is off.
	Machine string `json:"machine,omitempty"`
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered campaigns",
	Long: `List all campaigns registered in the global registry.

Campaigns are registered when created with 'camp init' or manually
with 'camp register'. The registry lives at ~/.obey/campaign/registry.json.

In a terminal, 'camp list' (with no flags) opens an interactive browser where you
can deactivate/reactivate campaigns (cycle lifecycle status), reassign their org,
and copy paths. Piped, with --json/--count, or with any filter/sort flag it
prints the table instead. Home paths display as '~'.

Output formats:
  table   - Aligned columns with headers (default)
  simple  - Campaign names only, one per line
  json    - JSON array for scripting

Sorting options:
  accessed - Most recently accessed first (default)
  name     - Alphabetically by name
  type     - Alphabetically by type
  org      - By org (fallback first, then alphabetical), then by name

Examples:
  camp list                  List all campaigns
  camp list --json           Output as JSON
  camp list --format json    Output as JSON
  camp list --sort name      Sort by name
  camp list --sort org       Sort by org, then name
  camp list --format simple  Names only for scripting
  camp list --count          Print only the total number of campaigns
  camp list --remote         Also list campaigns on machines in ~/.obey/machines.yaml

--remote runs each machine's own 'camp list --json' through a login shell
(sh -lc) so PATH entries a login profile exports (~/.profile, etc.) are
picked up. If camp still can't be found on a machine, set
CAMP_REMOTE_CAMP_PATH to its exact path there.`,
	Aliases: []string{"ls"},
	RunE:    runList,
}

var (
	listJSON   bool
	listCount  bool
	listRemote bool
)

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.GroupID = "registry"

	listCmd.Flags().StringP("format", "f", "table", "Output format (table, simple, json)")
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output as JSON (shorthand for --format json)")
	listCmd.Flags().BoolVar(&listCount, "count", false, "Print only the total number of campaigns")
	listCmd.Flags().StringP("sort", "s", "accessed", "Sort by (name, accessed, type, org)")
	listCmd.Flags().Bool("verify-verbose", false, "Show detailed verification output")
	listCmd.Flags().String("org", "", "Only campaigns in this org")
	listCmd.Flags().StringSlice("tag", nil, "Only campaigns carrying this tag (repeat for AND)")
	listCmd.Flags().String("status", "", "Only campaigns in this status (active, inactive, reference)")
	listCmd.Flags().Bool("all", false, "Show all statuses (default hides inactive/reference)")
	listCmd.Flags().Bool("group", false, "Force org grouping")
	listCmd.Flags().Bool("no-group", false, "Suppress org grouping")
	listCmd.Flags().BoolVar(&listRemote, "remote", false, "Also list campaigns on machines in ~/.obey/machines.yaml (ssh)")
	listCmd.Flags().BoolP("interactive", "i", false, "Open the interactive campaign browser (prints the table when stdout is not a terminal)")
	listCmd.Flags().String("path-output", "", "Write the selected campaign path to a file (shell integration)")
	_ = listCmd.Flags().MarkHidden("path-output")
}

func runList(cmd *cobra.Command, args []string) error {
	if listTUIRequested(cmd, stdoutIsTTY()) {
		return runListTUI(cmd)
	}
	return renderListTable(cmd)
}

func renderListTable(cmd *cobra.Command) error {
	ctx := cmd.Context()
	formatStr, _ := cmd.Flags().GetString("format")
	if listJSON {
		formatStr = "json"
	}

	filter, err := parseListFilter(cmd)
	if err != nil {
		return err
	}

	reg, report, err := loadVerifiedListRegistry(ctx)
	if err != nil {
		return err
	}

	if report.HasChanges() {
		verbose, _ := cmd.Flags().GetBool("verify-verbose")
		if verbose {
			printVerificationDetails(report)
		} else {
			printVerificationSummary(report)
		}
	}

	sortBy, _ := cmd.Flags().GetString("sort")
	campaigns := filterEntries(sortCampaigns(reg.Campaigns, sortBy, reg.FallbackOrg()), filter)

	var remoteResults []remoteResult
	if listRemote {
		for i := range campaigns {
			campaigns[i].Machine = machines.LocalMachineID
		}
		mf, err := machines.Load()
		if err != nil {
			return err
		}
		remoteResults = fanOutRemote(ctx, mf.Machines, enumerateRemoteFor(filter))
		for _, r := range remoteResults {
			if r.err == nil {
				campaigns = append(campaigns, r.rows...)
			}
			// A failed result becomes a labeled muted row in the human render
			// (task 2); it never contaminates the local/reachable rows here.
		}
		// Correctness backstop: a version-skewed or looser remote may return rows
		// outside the requested --org/--tag/--status, so re-apply the local filter
		// to the combined set. Idempotent for the already-filtered local rows.
		campaigns = filterEntries(campaigns, filter)
	}

	if listCount {
		if formatStr == "json" {
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(map[string]int{"count": len(campaigns)})
		}
		fmt.Println(ui.CountLabel(len(campaigns), "campaign", "campaigns"))
		return nil
	}

	if reg.Len() == 0 && !listRemote {
		if formatStr == "json" {
			return outputCampaigns(os.Stdout, []campaignEntry{}, formatStr)
		}
		fmt.Println(ui.Warning("No campaigns registered."))
		fmt.Println()
		fmt.Printf("  Create one with: %s\n", ui.Accent("camp init"))
		fmt.Printf("  Or register existing: %s\n", ui.Accent("camp register <path>"))
		return nil
	}

	if listRemote {
		return outputRemoteList(os.Stdout, cmd.ErrOrStderr(), campaigns, remoteResults, formatStr)
	}
	if formatStr == "json" {
		return outputCampaigns(os.Stdout, campaigns, formatStr)
	}
	if shouldGroup(cmd, campaigns) {
		return outputGrouped(campaigns, formatStr, reg.FallbackOrg())
	}
	return outputCampaigns(os.Stdout, campaigns, formatStr)
}

func loadVerifiedListRegistry(ctx context.Context) (*config.Registry, *config.VerificationReport, error) {
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return nil, nil, err
	}

	report, err := reg.VerifyAndRepair(ctx)
	if err != nil {
		return nil, nil, camperrors.Wrap(err, "registry verification failed")
	}

	if !report.HasChanges() {
		return reg, report, nil
	}

	if err := config.UpdateRegistry(ctx, func(locked *config.Registry) error {
		updatedReport, err := locked.VerifyAndRepair(ctx)
		if err != nil {
			return err
		}
		reg = locked
		report = updatedReport
		return nil
	}); err != nil {
		return nil, nil, camperrors.Wrap(err, "failed to save registry")
	}

	return reg, report, nil
}

func sortCampaigns(campaigns map[string]config.RegisteredCampaign, by, fallbackOrg string) []campaignEntry {
	entries := make([]campaignEntry, 0, len(campaigns))
	for id, c := range campaigns {
		tags := c.Tags
		if tags == nil {
			tags = []string{}
		}
		entries = append(entries, campaignEntry{
			ID:         id,
			Name:       c.Name,
			Type:       string(c.Type),
			Path:       c.Path,
			LastAccess: c.LastAccess,
			Org:        c.Org,
			Status:     c.Status,
			Tags:       tags,
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
	case "org":
		sort.Slice(entries, func(i, j int) bool {
			oi, oj := entries[i].Org, entries[j].Org
			if (oi == fallbackOrg) != (oj == fallbackOrg) {
				return oi == fallbackOrg
			}
			if oi != oj {
				return oi < oj
			}
			return entries[i].Name < entries[j].Name
		})
	default: // "accessed"
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].LastAccess.After(entries[j].LastAccess)
		})
	}

	return entries
}

func outputCampaigns(out io.Writer, campaigns []campaignEntry, format string) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(campaigns)
	case "simple":
		for _, c := range campaigns {
			if _, err := fmt.Fprintln(out, c.Name); err != nil {
				return err
			}
		}
		return nil
	default: // table
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			ui.Label("ID"), ui.Label("NAME"), ui.Label("ORG"), ui.Label("TYPE"), ui.Label("PATH")); err != nil {
			return err
		}
		for _, c := range campaigns {
			id, name, org, typ, path := campaignTableCells(c)
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, name, org, typ, path); err != nil {
				return err
			}
		}
		if err := w.Flush(); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		_, err := fmt.Fprintln(out, ui.Dim(ui.CountLabel(len(campaigns), "campaign", "campaigns")))
		return err
	}
}

func verificationSummaryText(r *config.VerificationReport) string {
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
	return strings.Join(parts, ", ")
}

func printVerificationSummary(r *config.VerificationReport) {
	fmt.Printf("%s Registry cleaned: %s\n\n", ui.SuccessIcon(), verificationSummaryText(r))
}

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
