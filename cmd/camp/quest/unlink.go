//go:build dev

package quest

import (
	"fmt"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
)

var questUnlinkCmd = &cobra.Command{
	Use:   "unlink <quest> <path>",
	Short: "Remove a linked artifact from a quest",
	Long: `Remove the association between a campaign artifact and a quest.

The path must match exactly as it was linked (campaign-root-relative).

Examples:
  camp quest unlink myquest .campaign/intents/inbox/some-intent.md
  camp quest unlink myquest projects/camp`,
	Args: cobra.ExactArgs(2),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive quest unlink operation",
	},
	RunE: runQuestUnlink,
}

func init() {
	Cmd.AddCommand(questUnlinkCmd)

	questUnlinkCmd.Flags().Bool("no-commit", false, "Don't create a git commit")
	questUnlinkCmd.ValidArgsFunction = completeQuestSelector
}

func runQuestUnlink(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	selector := args[0]
	path := args[1]
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return err
	}

	result, err := qctx.service.Unlink(ctx, selector, path)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Unlinked %s from quest %s\n", path, result.Quest.Name)

	if !noCommit {
		if err := autoCommitQuest(ctx, qctx, commit.QuestUnlink, result, "Unlinked "+path); err != nil {
			return camperrors.Wrap(err, "quest unlinked, but auto-commit failed")
		}
	}
	return nil
}
