package leverage

import "github.com/spf13/cobra"

// Cmd is the scaffold root for the leverage command family.
var Cmd = &cobra.Command{
	Use:     "leverage [directory]",
	Short:   "Compute leverage scores for a repository or camp",
	GroupID: "campaign",
	Long: `Compute productivity leverage scores by comparing scc COCOMO estimates
against actual development effort.

Leverage score measures how much more output you produce versus what
traditional estimation models predict for the same team and time.

  FullLeverage   = (EstimatedPeople x EstimatedMonths) / (ActualPeople x ElapsedMonths)
  SimpleLeverage = EstimatedPeople / ActualPeople

Pass a repository path for a read-only report without initializing a camp.
The default view is a repository summary; --author selects personal leverage,
and --by-author shows surviving line ownership by contributor. All three views
support --json. Ignore rules used by scc also control contributor attribution.
Uncommitted lines are accounted for separately. Commit spans estimate effort;
they do not measure hours worked or AI use.

Camp-wide aggregate leverage commands commit the data they write under .campaign/leverage so the
score history stays versioned without extra steps. Nothing outside that
directory is staged. Pass --no-commit to skip it once, or run
'camp leverage config --autocommit=false' to turn it off for the camp.

Examples:
  camp leverage                              Show team leverage (auto-detect authors from git)
  camp leverage --author lance@example.com   Show personal leverage
  camp leverage --project camp               Show score for specific project
  camp leverage --json                       Output as JSON
  camp leverage --people 2                   Override team size
  camp leverage --verbose                    Show diagnostic details
  camp leverage .                           Score current directory only
  camp leverage /repo --by-author            Show repository contributors
  camp leverage /repo --author alice@co      Show personal repository leverage
  camp leverage /repo --by-author --author-threshold 0 --json  Export all owners
  camp leverage --dir /path/to/repo          Score a specific directory`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
