//go:build dev

package quest

import (
	"fmt"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	questsvc "github.com/Obedience-Corp/camp/internal/quest"
)

var questUpdateCmd = &cobra.Command{
	Use:   "update <quest>",
	Short: "Update quest metadata",
	Long: `Update quest metadata without opening an editor.

Omitted fields are left unchanged. Provide an empty value to clear a field.

Examples:
  camp quest update platform-launch --purpose "harden launch path"
  camp quest update platform-launch --description ""
  camp quest update platform-launch --purpose "..." --description "..."`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive quest metadata update",
	},
	RunE: runQuestUpdate,
}

func init() {
	Cmd.AddCommand(questUpdateCmd)
	flags := questUpdateCmd.Flags()
	flags.String("purpose", "", "Short purpose statement")
	flags.String("description", "", "Full description")
	flags.Bool("no-commit", false, "Don't create a git commit")
	questUpdateCmd.ValidArgsFunction = completeQuestSelector
}

func runQuestUpdate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	var opts questsvc.MetadataUpdateOptions
	if cmd.Flags().Changed("purpose") {
		purpose, _ := cmd.Flags().GetString("purpose")
		opts.Purpose = &purpose
	}
	if cmd.Flags().Changed("description") {
		description, _ := cmd.Flags().GetString("description")
		opts.Description = &description
	}
	if opts.Purpose == nil && opts.Description == nil {
		return camperrors.New("at least one of --purpose or --description is required")
	}

	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return err
	}

	result, err := qctx.service.UpdateMetadata(ctx, args[0], opts)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Quest updated: %s\n", result.Quest.Name)
	if !noCommit {
		if err := autoCommitQuest(ctx, qctx, commit.QuestUpdate, result, "Updated quest metadata"); err != nil {
			return camperrors.Wrap(err, "quest updated, but auto-commit failed")
		}
	}
	return nil
}
