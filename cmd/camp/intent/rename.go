package intent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	wkcmd "github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/paths"
)

var intentRenameCmd = &cobra.Command{
	Use:   "rename <id> <new title>",
	Short: "Rename an idea",
	Long: `Rename an idea: update its title and regenerate its human-readable
filename. The idea's stable id is preserved, so references and lookups survive
the rename.

Resolution is by exact id (run 'camp idea list' to copy one).

Examples:
  camp idea rename add-dark-mode-20260119-153412 "Add a dark mode toggle"`,
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
		return camperrors.Wrap(err, "ensuring idea directories")
	}

	before, err := svc.Get(ctx, id)
	if err != nil {
		// Notes resolve through a separate store; fall back so a note can be
		// renamed by id too.
		before, err = svc.GetNote(ctx, id)
	}
	if err != nil {
		return camperrors.Wrapf(err, "idea not found: %s", id)
	}
	oldTitle := before.Title
	oldPath := before.Path

	renamed, err := svc.Rename(ctx, id, newTitle)
	if err != nil {
		return camperrors.Wrap(err, "failed to rename idea")
	}

	fmt.Printf("✓ Idea renamed: %s\n", renamed.Path)

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
		opts := wkcmd.AmbientCommitOptions(ctx, campaignRoot, cfg.ID, os.Stderr)
		opts.Files = commit.NormalizeFiles(campaignRoot, oldPath, renamed.Path, audit.FilePath(resolver.Intents()))
		opts.SelectiveOnly = true
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options:     opts,
			Action:      commit.IntentRename,
			IntentTitle: renamed.Title,
			Description: "Renamed from " + oldTitle,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
		commit.WarnIfSkipped(os.Stderr, commitResult)
	}

	return nil
}
