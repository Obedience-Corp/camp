//go:build dev

package quest

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
)

var questItemCmd = &cobra.Command{
	Use:   "item",
	Short: "Manage checklist items on a quest",
	Long: `Manage the ordered checklist items owned by a quest.

Items are the thin unit of work under a quest. Each item may optionally link a
workitem so one design/explore packet can back several slices of work.

Examples:
  camp quest item add samantha "Ship meeting recorder" --workitem samantha-meeting-recorder
  camp quest item done samantha 01eedb
  camp quest item rank samantha 01eedb 15
  camp quest item link-workitem samantha 01eedb samantha-remote-access`,
	RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
}

func init() {
	Cmd.AddCommand(questItemCmd)
	registerItemAdd()
	registerItemStatus()
	registerItemEdit()
	registerItemRank()
	registerItemWorkitem()
}

// completeQuestSelectorFirstArg completes quest selectors only for the first
// positional argument (the quest); later positions get no completion.
func completeQuestSelectorFirstArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeQuestSelector(cmd, args, toComplete)
}

func boolFlag(cmd *cobra.Command, name string) func() bool {
	return func() bool {
		v, _ := cmd.Flags().GetBool(name)
		return v
	}
}

func addCommonItemFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Emit JSON output")
	cmd.Flags().Bool("no-commit", false, "Don't create a git commit")
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QuestChecklistJSONVersion, boolFlag(cmd, "json")))
}

// emitChecklistMutation prints the human result (unless JSON), auto-commits, and
// emits the JSON payload when requested. Commit output is silenced in JSON mode.
func emitChecklistMutation(cmd *cobra.Command, qctx *questCommandContext, res *quest.ChecklistMutationResult, action commit.QuestAction, detail, humanLine string) error {
	ctx := cmd.Context()
	jsonOut, _ := cmd.Flags().GetBool("json")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	if !jsonOut && humanLine != "" {
		fmt.Fprintln(cmd.OutOrStdout(), humanLine)
	}
	if !noCommit {
		mr := &quest.MutationResult{Quest: res.Quest, Files: res.Files}
		if err := autoCommitQuestQuiet(ctx, qctx, action, mr, detail, jsonOut); err != nil {
			return camperrors.Wrap(err, "checklist updated, but auto-commit failed")
		}
	}
	if jsonOut {
		return outputChecklistItemResultJSON(ctx, cmd.OutOrStdout(), qctx.campaignRoot, res.Quest, res.Item)
	}
	return nil
}

// resolveWorkitemRef turns a user selector into a stored workitem reference.
func resolveWorkitemRef(ctx context.Context, root, sel string) (*quest.ChecklistWorkitem, error) {
	w, err := selector.Resolve(ctx, root, sel, selector.ResolveOptions{})
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolve workitem %q", sel)
	}
	id := w.StableID
	if id == "" {
		id = w.Key
	}
	ref := ""
	if w.StableID != "" {
		ref = workitem.Derive(w.StableID)
	}
	return &quest.ChecklistWorkitem{ID: id, Ref: ref}, nil
}

// --- add -------------------------------------------------------------------

func registerItemAdd() {
	cmd := &cobra.Command{
		Use:   "add <quest> <title>",
		Short: "Add a checklist item to a quest",
		Args:  cobra.ExactArgs(2),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Non-interactive checklist item creation",
		},
		ValidArgsFunction: completeQuestSelectorFirstArg,
	}
	cmd.Flags().String("workitem", "", "Link a workitem by id, key, path, or ref")
	cmd.Flags().String("notes", "", "Short note (depth belongs in the workitem)")
	cmd.Flags().String("status", "", "Initial status: open|doing|done|dropped (default open)")
	cmd.Flags().Int("rank", 0, "Explicit sort rank (default: appended after the last item)")
	addCommonItemFlags(cmd)
	cmd.RunE = jsoncontract.RunE(QuestChecklistJSONVersion, boolFlag(cmd, "json"), runItemAdd)
	questItemCmd.AddCommand(cmd)
}

func runItemAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	qctx, err := loadQuestCommandContext(ctx, true)
	if err != nil {
		return err
	}

	opts := quest.AddChecklistItemOptions{Title: args[1]}
	opts.Notes, _ = cmd.Flags().GetString("notes")
	if raw, _ := cmd.Flags().GetString("status"); raw != "" {
		status, err := quest.ParseChecklistItemStatus(raw)
		if err != nil {
			return err
		}
		opts.Status = status
	}
	if cmd.Flags().Changed("rank") {
		rank, _ := cmd.Flags().GetInt("rank")
		opts.Rank = &rank
	}
	if sel, _ := cmd.Flags().GetString("workitem"); sel != "" {
		wi, err := resolveWorkitemRef(ctx, qctx.campaignRoot, sel)
		if err != nil {
			return err
		}
		opts.Workitem = wi
	}

	res, err := qctx.service.AddChecklistItem(ctx, args[0], opts)
	if err != nil {
		return err
	}
	human := fmt.Sprintf("✓ Added %s [%s] %s", shortItemID(res.Item.ID), res.Item.Status, res.Item.Title)
	return emitChecklistMutation(cmd, qctx, res, commit.QuestChecklist, "Added checklist item", human)
}

// --- done / reopen ---------------------------------------------------------

func registerItemStatus() {
	done := &cobra.Command{
		Use:               "done <quest> <item>",
		Short:             "Mark a checklist item done",
		Args:              cobra.ExactArgs(2),
		Annotations:       map[string]string{"agent_allowed": "true", "agent_reason": "Non-interactive checklist update"},
		ValidArgsFunction: completeQuestSelectorFirstArg,
	}
	addCommonItemFlags(done)
	done.RunE = jsoncontract.RunE(QuestChecklistJSONVersion, boolFlag(done, "json"), runItemStatus(quest.ItemDone, "Completed checklist item", "✓ Done"))
	questItemCmd.AddCommand(done)

	reopen := &cobra.Command{
		Use:               "reopen <quest> <item>",
		Short:             "Reopen a completed checklist item",
		Args:              cobra.ExactArgs(2),
		Annotations:       map[string]string{"agent_allowed": "true", "agent_reason": "Non-interactive checklist update"},
		ValidArgsFunction: completeQuestSelectorFirstArg,
	}
	addCommonItemFlags(reopen)
	reopen.RunE = jsoncontract.RunE(QuestChecklistJSONVersion, boolFlag(reopen, "json"), runItemStatus(quest.ItemOpen, "Reopened checklist item", "↺ Reopened"))
	questItemCmd.AddCommand(reopen)
}

func runItemStatus(status quest.ChecklistItemStatus, detail, verb string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		qctx, err := loadQuestCommandContext(ctx, true)
		if err != nil {
			return err
		}
		res, err := qctx.service.SetChecklistItemStatus(ctx, args[0], args[1], status)
		if err != nil {
			return err
		}
		human := fmt.Sprintf("%s %s %s", verb, shortItemID(res.Item.ID), res.Item.Title)
		return emitChecklistMutation(cmd, qctx, res, commit.QuestChecklist, detail, human)
	}
}

// --- edit ------------------------------------------------------------------

func registerItemEdit() {
	cmd := &cobra.Command{
		Use:               "edit <quest> <item>",
		Short:             "Edit a checklist item's title, notes, or status",
		Args:              cobra.ExactArgs(2),
		Annotations:       map[string]string{"agent_allowed": "true", "agent_reason": "Non-interactive checklist update"},
		ValidArgsFunction: completeQuestSelectorFirstArg,
	}
	cmd.Flags().String("title", "", "New title")
	cmd.Flags().String("notes", "", "New note")
	cmd.Flags().String("status", "", "New status: open|doing|done|dropped")
	addCommonItemFlags(cmd)
	cmd.RunE = jsoncontract.RunE(QuestChecklistJSONVersion, boolFlag(cmd, "json"), runItemEdit)
	questItemCmd.AddCommand(cmd)
}

func runItemEdit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	qctx, err := loadQuestCommandContext(ctx, true)
	if err != nil {
		return err
	}

	var opts quest.EditChecklistItemOptions
	if cmd.Flags().Changed("title") {
		title, _ := cmd.Flags().GetString("title")
		opts.Title = &title
	}
	if cmd.Flags().Changed("notes") {
		notes, _ := cmd.Flags().GetString("notes")
		opts.Notes = &notes
	}
	if cmd.Flags().Changed("status") {
		raw, _ := cmd.Flags().GetString("status")
		status, err := quest.ParseChecklistItemStatus(raw)
		if err != nil {
			return err
		}
		opts.Status = &status
	}
	if opts.Title == nil && opts.Notes == nil && opts.Status == nil {
		return camperrors.New("nothing to edit: pass --title, --notes, or --status")
	}

	res, err := qctx.service.EditChecklistItem(ctx, args[0], args[1], opts)
	if err != nil {
		return err
	}
	human := fmt.Sprintf("✓ Updated %s [%s] %s", shortItemID(res.Item.ID), res.Item.Status, res.Item.Title)
	return emitChecklistMutation(cmd, qctx, res, commit.QuestChecklist, "Edited checklist item", human)
}

// --- rank ------------------------------------------------------------------

func registerItemRank() {
	cmd := &cobra.Command{
		Use:               "rank <quest> <item> <rank>",
		Short:             "Set a checklist item's sort rank",
		Args:              cobra.ExactArgs(3),
		Annotations:       map[string]string{"agent_allowed": "true", "agent_reason": "Non-interactive checklist update"},
		ValidArgsFunction: completeQuestSelectorFirstArg,
	}
	addCommonItemFlags(cmd)
	cmd.RunE = jsoncontract.RunE(QuestChecklistJSONVersion, boolFlag(cmd, "json"), runItemRank)
	questItemCmd.AddCommand(cmd)
}

func runItemRank(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	rank, err := strconv.Atoi(args[2])
	if err != nil {
		return camperrors.Wrapf(camperrors.ErrInvalidInput, "rank must be an integer, got %q", args[2])
	}
	qctx, err := loadQuestCommandContext(ctx, true)
	if err != nil {
		return err
	}
	res, err := qctx.service.RankChecklistItem(ctx, args[0], args[1], rank)
	if err != nil {
		return err
	}
	human := fmt.Sprintf("✓ Ranked %s → %d", shortItemID(res.Item.ID), res.Item.Rank)
	return emitChecklistMutation(cmd, qctx, res, commit.QuestChecklist, "Ranked checklist item", human)
}

// --- link-workitem / unlink-workitem --------------------------------------

func registerItemWorkitem() {
	link := &cobra.Command{
		Use:               "link-workitem <quest> <item> <selector>",
		Short:             "Link a workitem to a checklist item",
		Args:              cobra.ExactArgs(3),
		Annotations:       map[string]string{"agent_allowed": "true", "agent_reason": "Non-interactive checklist update"},
		ValidArgsFunction: completeQuestSelectorFirstArg,
	}
	addCommonItemFlags(link)
	link.RunE = jsoncontract.RunE(QuestChecklistJSONVersion, boolFlag(link, "json"), runItemLinkWorkitem)
	questItemCmd.AddCommand(link)

	unlink := &cobra.Command{
		Use:               "unlink-workitem <quest> <item>",
		Short:             "Remove the workitem link from a checklist item",
		Args:              cobra.ExactArgs(2),
		Annotations:       map[string]string{"agent_allowed": "true", "agent_reason": "Non-interactive checklist update"},
		ValidArgsFunction: completeQuestSelectorFirstArg,
	}
	addCommonItemFlags(unlink)
	unlink.RunE = jsoncontract.RunE(QuestChecklistJSONVersion, boolFlag(unlink, "json"), runItemUnlinkWorkitem)
	questItemCmd.AddCommand(unlink)
}

func runItemLinkWorkitem(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	qctx, err := loadQuestCommandContext(ctx, true)
	if err != nil {
		return err
	}
	wi, err := resolveWorkitemRef(ctx, qctx.campaignRoot, args[2])
	if err != nil {
		return err
	}
	res, err := qctx.service.LinkChecklistItemWorkitem(ctx, args[0], args[1], wi)
	if err != nil {
		return err
	}
	human := fmt.Sprintf("✓ Linked %s → %s", shortItemID(res.Item.ID), wi.ID)
	return emitChecklistMutation(cmd, qctx, res, commit.QuestChecklist, "Linked workitem to checklist item", human)
}

func runItemUnlinkWorkitem(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	qctx, err := loadQuestCommandContext(ctx, true)
	if err != nil {
		return err
	}
	res, err := qctx.service.UnlinkChecklistItemWorkitem(ctx, args[0], args[1])
	if err != nil {
		return err
	}
	human := fmt.Sprintf("✓ Unlinked workitem from %s", shortItemID(res.Item.ID))
	return emitChecklistMutation(cmd, qctx, res, commit.QuestChecklist, "Unlinked workitem from checklist item", human)
}
