//go:build dev

package quest

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	navtui "github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/quest"
	questtui "github.com/Obedience-Corp/camp/internal/quest/tui"
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

Examples:
  camp quest create                                    Interactive TUI
  camp quest create q2-reliability                     TUI with name pre-filled
  camp quest create q2-reliability --no-editor --purpose "harden platform"
  camp quest create data-pipeline --description "..."  Non-interactive
  camp quest create -e customer-onboarding             Deep capture with $EDITOR`,
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

	// Non-interactive path: flags provide all data
	if noEditor || description != "" {
		if name == "" {
			return camperrors.New("quest name is required in non-interactive mode\n       Provide a name argument or use the interactive TUI")
		}
		return createQuestDirect(cmd, name, purpose, description, tags, noCommit)
	}

	// Deep capture: open $EDITOR on YAML template
	if useEditor {
		return createQuestWithEditor(cmd, name, purpose, description, tags, noCommit)
	}

	// Interactive TUI path
	if !navtui.IsTerminal() {
		return camperrors.New("quest name is required in non-interactive mode\n       Provide a name argument or run in an interactive terminal")
	}

	model := questtui.NewQuestCreateModel(ctx, questtui.CreateOptions{
		DefaultName:    name,
		DefaultPurpose: purpose,
		DefaultTags:    tags,
	})

	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return camperrors.Wrap(err, "TUI error")
	}

	m, ok := finalModel.(questtui.QuestCreateModel)
	if !ok {
		return fmt.Errorf("unexpected model type: %T", finalModel)
	}

	if m.Cancelled() {
		return camperrors.New("quest creation cancelled")
	}

	result := m.Result()
	if result == nil {
		return camperrors.New("quest creation cancelled")
	}

	return createQuestDirect(cmd, result.Name, result.Purpose, result.Description, result.Tags, noCommit)
}

func createQuestDirect(cmd *cobra.Command, name, purpose, description, tags string, noCommit bool) error {
	qctx, err := loadQuestCommandContext(cmd.Context(), true)
	if err != nil {
		return err
	}

	result, err := qctx.service.Create(cmd.Context(), name, purpose, description, parseQuestTags(tags))
	if err != nil {
		return err
	}

	fmt.Printf("✓ Quest created: %s (%s)\n", result.Quest.Name, result.Quest.ID)
	fmt.Printf("  %s\n", quest.RelativePath(qctx.campaignRoot, result.Quest.Path))

	if !noCommit {
		if err := autoCommitQuest(cmd.Context(), qctx, commit.QuestCreate, result, "Created quest"); err != nil {
			return camperrors.Wrap(err, "quest created, but auto-commit failed")
		}
	}
	return nil
}

func createQuestWithEditor(cmd *cobra.Command, name, purpose, description, tags string, noCommit bool) error {
	if name == "" {
		return camperrors.New("quest name is required for --edit mode\n       Provide a name argument")
	}

	qctx, err := loadQuestCommandContext(cmd.Context(), true)
	if err != nil {
		return err
	}

	result, err := qctx.service.CreateWithEditor(cmd.Context(), name, purpose, description, parseQuestTags(tags), quest.OpenInEditor)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Quest created: %s (%s)\n", result.Quest.Name, result.Quest.ID)
	fmt.Printf("  %s\n", quest.RelativePath(qctx.campaignRoot, result.Quest.Path))

	if !noCommit {
		if err := autoCommitQuest(cmd.Context(), qctx, commit.QuestCreate, result, "Created quest"); err != nil {
			return camperrors.Wrap(err, "quest created, but auto-commit failed")
		}
	}
	return nil
}
