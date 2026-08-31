package leverage

import "github.com/spf13/cobra"

// Cmd is the scaffold root for the leverage command family.
var Cmd = &cobra.Command{
	Use:     "leverage [directory]",
	Short:   "Compute leverage scores for campaign projects",
	GroupID: "campaign",
	Long: `Compute productivity leverage scores by comparing scc COCOMO estimates
against actual development effort.

Leverage score measures how much more output you produce versus what
traditional estimation models predict for the same team and time.

  FullLeverage   = (EstimatedPeople x EstimatedMonths) / (ActualPeople x ElapsedMonths)
  SimpleLeverage = EstimatedPeople / ActualPeople

Leverage commands commit the data they write under .campaign/leverage so the
score history stays versioned without extra steps. Nothing outside that
directory is staged. Pass --no-commit to skip it once, or run
'camp leverage config --autocommit=false' to turn it off for the campaign.

Examples:
  camp leverage                              Show team leverage (auto-detect authors from git)
  camp leverage --author lance@example.com   Show personal leverage
  camp leverage --project camp               Show score for specific project
  camp leverage --json                       Output as JSON
  camp leverage --people 2                   Override team size
  camp leverage --verbose                    Show diagnostic details
  camp leverage .                            Score current directory only
  camp leverage --dir /path/to/repo          Score a specific directory`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
