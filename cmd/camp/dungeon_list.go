package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var dungeonListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List dungeon items",
	Long: `List items in the dungeon or parent items eligible for triage.

By default, lists items at the dungeon root (items already in the dungeon).
Use --triage to list parent directory items that could be moved into the dungeon.

OUTPUT FORMATS:
  table (default)   Human-readable table with columns
  simple            Names only, one per line (for scripting)
  json              Full metadata in JSON format

Examples:
  camp dungeon list                  List dungeon root items
  camp dungeon list --triage         List parent items eligible for triage
  camp dungeon list -f json          JSON output for scripting
  camp dungeon list -f simple        Names only, pipe to other commands`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive listing of dungeon items",
	},
	RunE: runDungeonList,
}

func init() {
	dungeonCmd.AddCommand(dungeonListCmd)

	flags := dungeonListCmd.Flags()
	flags.StringP("format", "f", "table", "Output format: table, simple, json")
	flags.Bool("triage", false, "List parent items eligible for triage into dungeon")
}

func runDungeonList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	format, _ := cmd.Flags().GetString("format")
	triageMode, _ := cmd.Flags().GetBool("triage")

	// Load campaign config
	_, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}
	dungeonPath := filepath.Join(cwd, "dungeon")

	svc := dungeon.NewService(campaignRoot, dungeonPath)

	if triageMode {
		items, err := svc.ListParentItems(ctx, cwd)
		if err != nil {
			return fmt.Errorf("listing parent items: %w", err)
		}
		return outputDungeonItems(items, format, "triage")
	}

	items, err := svc.ListItems(ctx)
	if err != nil {
		return fmt.Errorf("listing dungeon items: %w", err)
	}
	return outputDungeonItems(items, format, "dungeon")
}

func outputDungeonItems(items []dungeon.DungeonItem, format, source string) error {
	switch format {
	case "json":
		return outputDungeonJSON(items)
	case "simple":
		return outputDungeonSimple(items)
	default:
		return outputDungeonTable(items, source)
	}
}

func outputDungeonTable(items []dungeon.DungeonItem, source string) error {
	if len(items) == 0 {
		if source == "triage" {
			fmt.Println("No parent items eligible for triage.")
		} else {
			fmt.Println("Dungeon is empty.")
		}
		return nil
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)

	headers := []string{"NAME", "TYPE", "MODIFIED"}
	rows := make([][]string, 0, len(items))

	for _, item := range items {
		modified := formatDungeonTimestamp(item.ModTime)
		rows = append(rows, []string{
			item.Name,
			string(item.Type),
			modified,
		})
	}

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
	label := "dungeon item(s)"
	if source == "triage" {
		label = "parent item(s) eligible for triage"
	}
	fmt.Printf("\n%d %s\n", len(items), label)
	return nil
}

func outputDungeonSimple(items []dungeon.DungeonItem) error {
	for _, item := range items {
		fmt.Println(item.Name)
	}
	return nil
}

func outputDungeonJSON(items []dungeon.DungeonItem) error {
	type jsonItem struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Type    string `json:"type"`
		ModTime string `json:"mod_time"`
	}

	output := make([]jsonItem, 0, len(items))
	for _, item := range items {
		output = append(output, jsonItem{
			Name:    item.Name,
			Path:    item.Path,
			Type:    string(item.Type),
			ModTime: item.ModTime.Format(time.RFC3339),
		})
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

func formatDungeonTimestamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("Jan 02 15:04")
}
