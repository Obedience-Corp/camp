package intent

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/paths"
)

var intentConvertCmd = &cobra.Command{
	Use:        "convert <id>",
	Short:      "Convert a note into an intent",
	Hidden:     true,
	Deprecated: "unused; edit the note's frontmatter type and move it into inbox/ by hand",
	Long: `Promote a note into the intent lifecycle.

A note lives outside the inbox → ready → active lifecycle. Converting it moves
the note into inbox/ and attaches an intent type, after which it behaves like
any other intent. This is the only bridge from a note into the lifecycle.

Examples:
  camp intent convert check-daemon-socket --type idea
  camp intent convert check-daemon-socket -t feature`,
	Args: cobra.ExactArgs(1),
	RunE: runIntentConvert,
}

func init() {
	Cmd.AddCommand(intentConvertCmd)

	intentConvertCmd.Flags().StringP("type", "t", "idea", "Intent type to attach (idea, feature, bug, research, chore)")
	intentConvertCmd.Flags().Bool("no-commit", false, "Don't create a git commit")
}

func runIntentConvert(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]
	typeFlag, _ := cmd.Flags().GetString("type")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())
	if err := svc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring intent directories")
	}

	note, err := svc.GetNote(ctx, id)
	if err != nil {
		return camperrors.Wrapf(err, "note not found: %s", id)
	}
	sourcePath := note.Path

	result, err := svc.Convert(ctx, id, intent.Type(typeFlag))
	if err != nil {
		return camperrors.Wrap(err, "failed to convert note")
	}

	fmt.Printf("✓ Note converted to %s intent: %s\n", typeFlag, result.Path)

	if err := appendIntentAuditEvent(ctx, resolver.Intents(), audit.Event{
		Type:   audit.EventMove,
		ID:     result.ID,
		Title:  result.Title,
		From:   string(intent.StatusNote),
		To:     string(intent.StatusInbox),
		Reason: "converted note to " + typeFlag,
	}); err != nil {
		return err
	}

	if !noCommit {
		files := commit.NormalizeFiles(campaignRoot, sourcePath, result.Path, audit.FilePath(resolver.Intents()))
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options: commit.Options{
				CampaignRoot:  campaignRoot,
				CampaignID:    cfg.ID,
				Files:         files,
				SelectiveOnly: true,
			},
			Action:      commit.IntentMove,
			IntentTitle: result.Title,
			Description: "Converted note to " + typeFlag + " intent",
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}
