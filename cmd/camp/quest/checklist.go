//go:build dev

package quest

import (
	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/quest"
)

var (
	checklistJSONFlag bool
	checklistOpenOnly bool
)

var questChecklistCmd = &cobra.Command{
	Use:   "checklist <quest>",
	Short: "List the checklist for a quest",
	Long: `List the checklist (workitem-aware todos) owned by a quest.

A checklist is the thin layer between a quest and its workitems: an ordered set
of work units, each optionally linked to a workitem, so an initiative can be
divided across agents and sessions without promoting everything into a festival.

Examples:
  camp quest checklist samantha
  camp quest checklist samantha --open
  camp quest checklist samantha --json`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive checklist listing",
	},
	ValidArgsFunction: completeQuestSelector,
	RunE:              jsoncontract.RunE(QuestChecklistJSONVersion, func() bool { return checklistJSONFlag }, runQuestChecklist),
}

func init() {
	Cmd.AddCommand(questChecklistCmd)
	questChecklistCmd.Flags().BoolVar(&checklistJSONFlag, "json", false, "Emit JSON output")
	questChecklistCmd.Flags().BoolVar(&checklistOpenOnly, "open", false, "Show only open items (not done or dropped)")
	questChecklistCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QuestChecklistJSONVersion, func() bool { return checklistJSONFlag }))
}

func runQuestChecklist(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	qctx, err := loadQuestCommandContext(ctx, true)
	if err != nil {
		return err
	}

	q, cl, err := qctx.service.Checklist(ctx, args[0])
	if err != nil {
		return camperrors.Wrapf(err, "load checklist for %q", args[0])
	}

	items := cl.Items
	if checklistOpenOnly {
		items = filterOpenItems(items)
	}

	if checklistJSONFlag {
		return outputChecklistJSON(ctx, cmd.OutOrStdout(), qctx.campaignRoot, q, items)
	}
	return outputChecklistTable(ctx, cmd.OutOrStdout(), qctx.campaignRoot, q, items)
}

func filterOpenItems(items []quest.ChecklistItem) []quest.ChecklistItem {
	out := make([]quest.ChecklistItem, 0, len(items))
	for _, item := range items {
		if !item.Status.Terminal() {
			out = append(out, item)
		}
	}
	return out
}
