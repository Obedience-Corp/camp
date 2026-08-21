//go:build dev

package quest

import (
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/spf13/cobra"
)

const QuestListJSONVersion = "quest-list/v1alpha1"

var (
	questListAll      bool
	questListDungeon  bool
	questListJSON     bool
	questListStatuses []string
)

var questListCmd = &cobra.Command{
	Use:   "list",
	Short: "List quests",
	Long: `List quests in the current campaign.

In a terminal, 'camp quest list' (with no flags) opens an interactive
browser that uses the same TUI components as 'camp list' (boxed frame,
status-grouped rows, collapsing help). Press j/k to move, y to copy the
quest path, f to cycle live / all / dungeon, and g or enter to jump to
the selected quest when shell integration is loaded:

  eval "$(camp shell-init zsh)"   # or bash / sh
  camp shell-init fish | source   # fish
  camp quest list                 # interactive browser; g cds into the quest

Piped or with --json it prints that format instead. --status/--all/--dungeon
print the filtered table. -i forces the browser (and still prints the table
when stdout is not a terminal).`,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive quest listing",
	},
	RunE: jsoncontract.RunE(QuestListJSONVersion, func() bool { return questListJSON }, runQuestList),
}

func init() {
	Cmd.AddCommand(questListCmd)
	questListCmd.Flags().BoolVar(&questListAll, "all", false, "Include dungeon quests")
	questListCmd.Flags().BoolVar(&questListDungeon, "dungeon", false, "Show only dungeon quests")
	questListCmd.Flags().BoolVar(&questListJSON, "json", false, "Emit JSON output")
	questListCmd.Flags().StringSliceVar(&questListStatuses, "status", nil, "Filter by quest status")
	questListCmd.Flags().BoolP("interactive", "i", false,
		"Open the interactive quest browser (prints the table when stdout is not a terminal)")
	questListCmd.Flags().String("path-output", "", "Write the selected quest path to a file (shell integration)")
	_ = questListCmd.Flags().MarkHidden("path-output")
	questListCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QuestListJSONVersion, func() bool { return questListJSON }))
}

func runQuestList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	// Ensure the quest scaffold (and the default quest) exists before listing so
	// a freshly-opened campaign always has at least one quest instead of failing
	// with ErrNotInitialized. This is what guarantees "you always have a quest".
	qctx, err := loadQuestCommandContext(ctx, true)
	if err != nil {
		return err
	}

	statuses, err := parseQuestStatuses(questListStatuses)
	if err != nil {
		return err
	}

	// The browser holds every quest and applies live/all/dungeon as a live
	// filter (the way camp worktrees list loads unfiltered so f can toggle
	// stale). --json and the shaping flags still print the table/JSON.
	openTUI := questListTUIRequested(cmd, stdoutIsTTY())
	opts := &quest.ListOptions{
		Statuses: statuses,
		All:      questListAll,
		Dungeon:  questListDungeon,
	}
	if openTUI {
		opts = &quest.ListOptions{All: true}
	}

	quests, err := qctx.service.List(ctx, opts)
	if err != nil {
		return err
	}

	if questListJSON {
		return outputQuestListJSON(qctx, quests)
	}
	if openTUI {
		return runQuestListTUI(cmd, qctx, quests, statuses)
	}
	return outputQuestTable(qctx, quests)
}
