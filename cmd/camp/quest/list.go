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
	questListCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QuestListJSONVersion, func() bool { return questListJSON }))
}

func runQuestList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return err
	}

	statuses, err := parseQuestStatuses(questListStatuses)
	if err != nil {
		return err
	}

	quests, err := qctx.service.List(ctx, &quest.ListOptions{
		Statuses: statuses,
		All:      questListAll,
		Dungeon:  questListDungeon,
	})
	if err != nil {
		return err
	}

	if questListJSON {
		return outputQuestListJSON(qctx, quests)
	}
	return outputQuestTable(qctx, quests)
}
