//go:build dev

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/quest"
)

var questEditCmd = &cobra.Command{
	Use:   "edit <quest>",
	Short: "Edit an existing quest",
	Long: `Edit quest metadata in your preferred editor.

This opens a temporary YAML file, validates the edited result, and writes the
updated quest back to its canonical location.

Examples:
  camp quest edit runtime-hardening`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Interactive editor workflow",
		"interactive":   "true",
	},
	RunE: runQuestEdit,
}

func init() {
	questCmd.AddCommand(questEditCmd)
	questEditCmd.Flags().Bool("no-commit", false, "Don't create a git commit")
	questEditCmd.ValidArgsFunction = completeQuestSelector
}

func runQuestEdit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return err
	}

	result, err := qctx.service.Edit(ctx, args[0], quest.OpenInEditor)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Quest updated: %s\n", quest.RelativePath(qctx.campaignRoot, result.Quest.Path))
	if !noCommit {
		if err := autoCommitQuest(ctx, qctx, commit.QuestEdit, result, "Edited quest metadata"); err != nil {
			return fmt.Errorf("quest updated, but auto-commit failed: %w", err)
		}
	}
	return nil
}
