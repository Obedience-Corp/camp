//go:build dev

package quest

import (
	"fmt"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
)

var questRenameCmd = &cobra.Command{
	Use:   "rename <quest> <new-name>",
	Short: "Rename a quest",
	Long: `Rename a quest without changing its immutable directory slug.

Examples:
  camp quest rename cost-reduction infrastructure-efficiency`,
	Args: cobra.ExactArgs(2),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive quest metadata update",
	},
	RunE: runQuestRename,
}

func init() {
	Cmd.AddCommand(questRenameCmd)
	questRenameCmd.Flags().Bool("no-commit", false, "Don't create a git commit")
	questRenameCmd.ValidArgsFunction = completeQuestSelector
}

func runQuestRename(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return err
	}

	result, err := qctx.service.Rename(ctx, args[0], args[1])
	if err != nil {
		return err
	}

	fmt.Printf("✓ Quest renamed: %s\n", result.Quest.Name)
	if !noCommit {
		if err := autoCommitQuest(ctx, qctx, commit.QuestRename, result, "Renamed quest"); err != nil {
			return camperrors.Wrap(err, "quest renamed, but auto-commit failed")
		}
	}
	return nil
}
