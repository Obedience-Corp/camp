package intent

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/paths"
)

var intentRenameCmd = &cobra.Command{
	Use:   "rename <id> <new title>",
	Short: "Rename an intent",
	Long: `Rename an intent: update its title and regenerate its human-readable
filename. The intent's stable id is preserved, so references and lookups survive
the rename.

Examples:
  camp intent rename add-dark "Add a dark mode toggle"`,
	Args: cobra.ExactArgs(2),
	RunE: runIntentRename,
}

func init() {
	Cmd.AddCommand(intentRenameCmd)
	intentRenameCmd.Flags().Bool("no-commit", false, "Don't create a git commit")
}

func runIntentRename(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]
	newTitle := args[1]
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

	before, err := svc.Get(ctx, id)
	if err != nil {
		return camperrors.Wrapf(err, "intent not found: %s", id)
	}
	oldTitle := before.Title
	oldPath := before.Path

	renamed, err := svc.Rename(ctx, id, newTitle)
	if err != nil {
		return camperrors.Wrap(err, "failed to rename intent")
	}

	fmt.Printf("✓ Intent renamed: %s\n", renamed.Path)

	if err := appendIntentAuditEvent(ctx, resolver.Intents(), audit.Event{
		Type:  audit.EventRename,
		ID:    renamed.ID,
		Title: renamed.Title,
		Changes: []audit.FieldChange{
			{Field: "title", Old: oldTitle, New: renamed.Title},
			{Field: "filename", Old: filepath.Base(oldPath), New: filepath.Base(renamed.Path)},
		},
	}); err != nil {
		return err
	}

	if !noCommit {
		files := commit.NormalizeFiles(campaignRoot, oldPath, renamed.Path, audit.FilePath(resolver.Intents()))
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options: commit.Options{
				CampaignRoot:  campaignRoot,
				CampaignID:    cfg.ID,
				Files:         files,
				SelectiveOnly: true,
			},
			Action:      commit.IntentRename,
			IntentTitle: renamed.Title,
			Description: "Renamed from " + oldTitle,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}
