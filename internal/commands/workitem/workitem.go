package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
	wktui "github.com/Obedience-Corp/camp/internal/workitem/tui"
)

// NewWorkitemCommand creates the camp workitem command.
func NewWorkitemCommand() *cobra.Command {
	var (
		flagJSON   bool
		flagPrint  bool
		flagTypes  []string
		flagStages []string
		flagLimit  int
		flagQuery  string
	)

	cmd := &cobra.Command{
		Use:     "workitem",
		Aliases: []string{"wi", "workitems"},
		Short:   "View active campaign work items",
		Long: `View active campaign work items across intents, designs, explore, and festivals.

Default mode launches an interactive TUI dashboard. Use --json for machine-readable
output or --print to select and print a path for shell integration.

Examples:
  camp workitem                              # interactive dashboard
  camp workitem --json                       # JSON output for agents/scripts
  camp workitem --json --type design         # filter by type
  camp workitem --json --type intent --limit 5
  camp workitem --print                      # select and print path`,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Supports --json for non-interactive output",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if err := validateFlags(flagJSON, flagPrint, flagTypes, flagStages); err != nil {
				return err
			}

			interactive := isInteractive()
			if !interactive && !flagJSON && !flagPrint {
				return fmt.Errorf("non-interactive use requires --json or --print flag")
			}

			cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign directory")
			}
			resolver := paths.NewResolverFromConfig(campaignRoot, cfg)

			items, err := wkitem.Discover(ctx, campaignRoot, resolver)
			if err != nil {
				return camperrors.Wrap(err, "discovering work items")
			}

			// Load priority store and prune stale entries against full discovery set.
			storePath := priority.StorePath(campaignRoot)
			store, err := priority.Load(storePath)
			if err != nil {
				return camperrors.Wrap(err, "loading priority store")
			}
			validKeys := make(map[string]bool, len(items))
			for _, item := range items {
				validKeys[item.Key] = true
			}
			if priority.Prune(store, validKeys) {
				if err := priority.SaveOrDelete(storePath, store); err != nil {
					return camperrors.Wrap(err, "saving pruned priority store")
				}
			}

			// Apply priority overlay and re-sort with priority buckets.
			items = priority.Apply(store, items)
			wkitem.Sort(items)

			items = wkitem.Filter(items, flagTypes, flagStages, flagQuery)
			if flagLimit > 0 && flagLimit < len(items) {
				items = items[:flagLimit]
			}

			switch {
			case flagJSON:
				return outputJSON(campaignRoot, items)
			case flagPrint && !interactive:
				// Non-interactive --print: output first item path directly
				if len(items) == 0 {
					return fmt.Errorf("no work items found")
				}
				fmt.Println(items[0].AbsPath(campaignRoot))
				return nil
			case flagPrint:
				return runTUI(ctx, items, true, campaignRoot, resolver, store, storePath)
			default:
				return runTUI(ctx, items, false, campaignRoot, resolver, store, storePath)
			}
		},
	}

	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&flagPrint, "print", false, "Print path only (for shell integration)")
	cmd.Flags().StringArrayVar(&flagTypes, "type", nil, "Filter by workflow type (intent, design, explore, festival)")
	cmd.Flags().StringArrayVar(&flagStages, "stage", nil, "Filter by lifecycle stage (inbox, active, ready, planning)")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Maximum number of items to return")
	cmd.Flags().StringVar(&flagQuery, "query", "", "Search query to filter items")

	return cmd
}

func isInteractive() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func validateFlags(jsonMode, printMode bool, types, stages []string) error {
	validTypes := map[string]bool{"intent": true, "design": true, "explore": true, "festival": true}
	for _, t := range types {
		if !validTypes[t] {
			return fmt.Errorf("unknown --type value: %q (valid: intent, design, explore, festival)", t)
		}
	}
	validStages := map[string]bool{"inbox": true, "active": true, "ready": true, "planning": true}
	for _, s := range stages {
		if !validStages[s] {
			return fmt.Errorf("unknown --stage value: %q (valid: inbox, active, ready, planning)", s)
		}
	}
	if jsonMode && printMode {
		return fmt.Errorf("--json and --print are mutually exclusive")
	}
	return nil
}

func outputJSON(campaignRoot string, items []wkitem.WorkItem) error {
	payload := wkitem.NewPayload(campaignRoot, items)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func runTUI(ctx context.Context, items []wkitem.WorkItem, printOnly bool, campaignRoot string, resolver *paths.Resolver, store *priority.Store, storePath string) error {
	if len(items) == 0 {
		return fmt.Errorf("no work items found")
	}

	model := wktui.New(ctx, items, campaignRoot, resolver, store, storePath)
	p := tea.NewProgram(model, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return camperrors.Wrap(err, "TUI error")
	}
	m, ok := result.(wktui.Model)
	if !ok || m.Selected == nil {
		return nil
	}
	if printOnly {
		fmt.Println(m.Selected.AbsPath(campaignRoot))
	} else {
		fmt.Printf("cd %s\n", m.Selected.AbsPath(campaignRoot))
	}
	return nil
}
