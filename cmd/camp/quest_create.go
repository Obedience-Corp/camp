//go:build dev

package main

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var questCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new quest",
	Long: `Create a new quest for a bounded execution context.

Provide --purpose/--description/--tags to create non-interactively.
Without --no-editor, camp opens your preferred editor on a YAML quest template.

Examples:
  camp quest create runtime-hardening --no-editor --purpose "stabilize runtime"
  camp quest create ui-refresh --description "Refine workspace layout"
  camp quest create daemon-pivot`,
	Args: cobra.MaximumNArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Agents can use --no-editor or --description for non-interactive quest creation",
	},
	RunE: runQuestCreate,
}

func init() {
	questCmd.AddCommand(questCreateCmd)

	flags := questCreateCmd.Flags()
	flags.String("purpose", "", "Short purpose statement")
	flags.String("description", "", "Full description")
	flags.String("tags", "", "Comma-separated tags")
	flags.Bool("no-editor", false, "Skip editor and create directly from flags")
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
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	if name == "" {
		if !tui.IsTerminal() {
			return fmt.Errorf("quest name is required in non-interactive mode\n       Provide a name argument or run in an interactive terminal")
		}
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().Title("Quest Name").Value(&name),
				huh.NewInput().Title("Purpose").Description("Optional short purpose statement").Value(&purpose),
			),
		)
		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				return fmt.Errorf("quest creation cancelled")
			}
			return err
		}
		if name == "" {
			return fmt.Errorf("quest name is required")
		}
	}

	qctx, err := loadQuestCommandContext(ctx, true)
	if err != nil {
		return err
	}

	var result *quest.MutationResult
	if noEditor || description != "" {
		result, err = qctx.service.Create(ctx, name, purpose, description, parseQuestTags(tags))
	} else {
		result, err = qctx.service.CreateWithEditor(ctx, name, purpose, description, parseQuestTags(tags), quest.OpenInEditor)
	}
	if err != nil {
		return err
	}

	fmt.Printf("✓ Quest created: %s (%s)\n", result.Quest.Name, result.Quest.ID)
	fmt.Printf("  %s\n", quest.RelativePath(qctx.campaignRoot, result.Quest.Path))

	if !noCommit {
		autoCommitQuest(ctx, qctx, commit.QuestCreate, result, "Created quest")
	}
	return nil
}
