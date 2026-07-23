//go:build dev

package quest

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/ledger"
	navtui "github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/quest"
	questtui "github.com/Obedience-Corp/camp/internal/quest/tui"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

var questCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new quest",
	Long: `Create a new working context within a campaign.

A quest is a long-lived scope of work — not a single feature or task, but a
broader initiative that may span projects, sessions, and festivals.

CAPTURE MODES:
  TUI (default)         Step-through form (name, purpose, description, tags)
  Non-interactive       Provide --no-editor or --description for agent use
  Deep (--edit)         Full YAML template in $EDITOR

A quest may optionally be bound to a workitem at creation time. Pass
--workitem <selector> to bind non-interactively (the selector accepts a
workitem ref, stable id, key, path, directory slug, or festival id), or pick
one from the optional binding step in the interactive TUI. The binding is
stored exactly as "camp quest link" would store it, so it renders through
"camp quest show" and "camp quest links" with no extra steps.

Examples:
  camp quest create                                    Interactive TUI
  camp quest create q2-reliability                     TUI with name pre-filled
  camp quest create q2-reliability --no-editor --purpose "harden platform"
  camp quest create data-pipeline --description "..."  Non-interactive
  camp quest create -e customer-onboarding             Deep capture with $EDITOR
  camp quest create launch --no-editor --workitem SC0001  Bind a festival workitem`,
	Args: cobra.MaximumNArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Agents can use --no-editor or --description for non-interactive quest creation",
	},
	RunE: runQuestCreate,
}

func init() {
	Cmd.AddCommand(questCreateCmd)

	flags := questCreateCmd.Flags()
	flags.String("purpose", "", "Short purpose statement")
	flags.String("description", "", "Full description")
	flags.String("tags", "", "Comma-separated tags")
	flags.Bool("no-editor", false, "Skip editor and create directly from flags")
	flags.BoolP("edit", "e", false, "Open in $EDITOR for deep capture")
	flags.Bool("no-commit", false, "Don't create a git commit")
	flags.String("workitem", "", "Bind a workitem by selector (ref, id, key, path, slug, or festival id)")
	_ = questCreateCmd.RegisterFlagCompletionFunc("workitem", completeWorkitemSelector)
}

func runQuestCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	purpose, _ := cmd.Flags().GetString("purpose")
	description, _ := cmd.Flags().GetString("description")
	tags, _ := cmd.Flags().GetString("tags")
	noEditor, _ := cmd.Flags().GetBool("no-editor")
	useEditor, _ := cmd.Flags().GetBool("edit")
	noCommit, _ := cmd.Flags().GetBool("no-commit")
	workitemSel, _ := cmd.Flags().GetString("workitem")

	// Resolve the --workitem binding up-front so a bad selector fails before we
	// open the TUI or create anything.
	var boundPath string
	if workitemSel != "" {
		resolved, err := resolveWorkitemPath(ctx, workitemSel)
		if err != nil {
			return err
		}
		boundPath = resolved
	}

	// Non-interactive path: flags provide all data
	if noEditor || description != "" {
		if name == "" {
			return camperrors.New("quest name is required in non-interactive mode (provide a name argument or run interactively)")
		}
		return createQuestDirect(cmd, name, purpose, description, tags, noCommit, boundPath)
	}

	// Deep capture: open $EDITOR on YAML template
	if useEditor {
		return createQuestWithEditor(cmd, name, purpose, description, tags, noCommit, boundPath)
	}

	// Interactive TUI path
	if !navtui.IsTerminal() {
		return camperrors.New("quest name is required in non-interactive mode (provide a name argument or run interactively)")
	}

	// Only offer the picker when the binding was not already fixed by the flag.
	var choices []questtui.WorkitemChoice
	if boundPath == "" {
		if gathered, err := gatherWorkitemChoices(ctx); err == nil {
			choices = gathered
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: workitem picker unavailable: %v\n", err)
		}
	}

	model := questtui.NewQuestCreateModel(ctx, questtui.CreateOptions{
		DefaultName:    name,
		DefaultPurpose: purpose,
		DefaultTags:    tags,
		Choices:        choices,
	})

	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return camperrors.Wrap(err, "TUI error")
	}

	m, ok := finalModel.(questtui.QuestCreateModel)
	if !ok {
		return camperrors.New("unexpected TUI model type returned")
	}

	if m.Cancelled() {
		return camperrors.New("quest creation cancelled")
	}

	result := m.Result()
	if result == nil {
		return camperrors.New("quest creation cancelled")
	}

	if boundPath == "" {
		boundPath = result.WorkitemPath
	}
	return createQuestDirect(cmd, result.Name, result.Purpose, result.Description, result.Tags, noCommit, boundPath)
}

func createQuestDirect(cmd *cobra.Command, name, purpose, description, tags string, noCommit bool, boundPath string) error {
	qctx, err := loadQuestCommandContext(cmd.Context(), true)
	if err != nil {
		return err
	}

	result, err := qctx.service.Create(cmd.Context(), name, purpose, description, parseQuestTags(tags))
	if err != nil {
		return err
	}

	result, err = bindWorkitem(cmd.Context(), qctx, result, boundPath)
	if err != nil {
		return err
	}

	emitQuestCreated(cmd, qctx, result)
	printQuestCreated(qctx, result, boundPath)

	if !noCommit {
		if err := autoCommitQuest(cmd.Context(), qctx, commit.QuestCreate, result, "Created quest"); err != nil {
			return camperrors.Wrap(err, "quest created, but auto-commit failed")
		}
	}
	return nil
}

func createQuestWithEditor(cmd *cobra.Command, name, purpose, description, tags string, noCommit bool, boundPath string) error {
	if name == "" {
		return camperrors.New("quest name is required for --edit mode (provide a name argument)")
	}

	qctx, err := loadQuestCommandContext(cmd.Context(), true)
	if err != nil {
		return err
	}

	result, err := qctx.service.CreateWithEditor(cmd.Context(), name, purpose, description, parseQuestTags(tags), quest.OpenInEditor)
	if err != nil {
		return err
	}

	result, err = bindWorkitem(cmd.Context(), qctx, result, boundPath)
	if err != nil {
		return err
	}

	emitQuestCreated(cmd, qctx, result)
	printQuestCreated(qctx, result, boundPath)

	if !noCommit {
		if err := autoCommitQuest(cmd.Context(), qctx, commit.QuestCreate, result, "Created quest"); err != nil {
			return camperrors.Wrap(err, "quest created, but auto-commit failed")
		}
	}
	return nil
}

// bindWorkitem links a resolved workitem path to a freshly created quest through
// the same service path `camp quest link` uses (auto-detecting the link type).
// It returns the post-link MutationResult so the binding lands in the same
// create commit. A no-op when boundPath is empty.
func bindWorkitem(ctx context.Context, qctx *questCommandContext, result *quest.MutationResult, boundPath string) (*quest.MutationResult, error) {
	if boundPath == "" {
		return result, nil
	}
	linked, err := qctx.service.Link(ctx, result.Quest.ID, boundPath, "")
	if err != nil {
		return nil, camperrors.Wrapf(err, "quest created, but workitem binding failed for %s", boundPath)
	}
	return linked, nil
}

func emitQuestCreated(cmd *cobra.Command, qctx *questCommandContext, result *quest.MutationResult) {
	ledger.NewFromRoot(cmd.Context(), qctx.campaignRoot, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(cmd.Context(), ledgerkit.KindCreated, ledgerkit.Scope{Quest: result.Quest.ID}, ledger.WithWhy(result.Quest.Name))
}

func printQuestCreated(qctx *questCommandContext, result *quest.MutationResult, boundPath string) {
	fmt.Printf("✓ Quest created: %s (%s)\n", result.Quest.Name, result.Quest.ID)
	fmt.Printf("  %s\n", quest.RelativePath(qctx.campaignRoot, result.Quest.Path))
	if boundPath != "" {
		fmt.Printf("  bound workitem: %s\n", boundPath)
	}
}
