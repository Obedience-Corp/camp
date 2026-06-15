package intent

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var intentCountCmd = newIntentCountCommand()

func newIntentCountCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "count",
		Short: "Count intents by status directory",
		Long: `Display a count of intents grouped by status directory.

OUTPUT FORMATS:
  table (default)   Styled summary with counts per status
  json              Machine-readable JSON output

Examples:
  camp intent count              Show counts per status
  camp intent count -f json      JSON output for scripting`,
	}
	jsonRequested := func() bool { return intentJSONRequested(cmd, &jsonOut) }
	cmd.Args = jsoncontract.Args(IntentJSONVersion, jsonRequested, cobra.NoArgs)
	cmd.RunE = jsoncontract.RunE(IntentJSONVersion, jsonRequested, runIntentCount)
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(IntentJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().StringP("format", "f", "table", "Output format: table, json")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func init() {
	Cmd.AddCommand(intentCountCmd)
}

func runIntentCount(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	format, _ := cmd.Flags().GetString("format")
	jsonOut, _ := cmd.Flags().GetBool("json")

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	campaignRoot, err = pathutil.ResolveRoot(campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "resolving campaign root")
	}

	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())
	warnPendingLegacyMigration(svc)

	counts, total, err := svc.Count(ctx)
	if err != nil {
		return camperrors.Wrap(err, "counting intents")
	}

	switch {
	case jsonOut || format == "json":
		return outputIntentCountPayload(cmd.OutOrStdout(), campaignRoot, counts, total)
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
