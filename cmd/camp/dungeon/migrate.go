package dungeon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon/migrate"
	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var (
	migrateDryRun   bool
	migrateNoCommit bool
)

var dungeonMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Convert every campaign dungeon to the hidden .dungeon spelling",
	Long: `Convert every dungeon in this campaign from "dungeon" to ".dungeon".

New campaigns hide the dungeon so it stops being the first thing newcomers ask
about. This converts a campaign made before that change. A campaign uses one
spelling throughout, so the sweep covers every dungeon at once: the campaign
root, festivals/, .campaign/intents/, .campaign/quests/, and each workflow
type. Dungeons are discovered on disk, so locations added since this command
was written are included too.

The move goes through git, so history and rename detection survive, and lands
as a single commit you can revert.

projects/ is never touched. Projects own their own trees, and a source
directory named "dungeon" inside one is not a campaign dungeon.

Release ordering matters when a campaign contains festivals/: this command
also renames festivals/dungeon. Do not run it against a campaign used by a
fest build that does not understand .dungeon. Land fest#274 and ship a fest
release with the matching support before making this migration available to
users.

Nothing is moved unless everything can be: if any location holds both
spellings, or a .dungeon is already in the way, the command reports it and
exits without changing anything.`,
	Example: `  camp dungeon migrate            Convert and commit
  camp dungeon migrate --dry-run  Show what would move, change nothing
  camp dungeon migrate --no-commit  Convert, leave the changes staged`,
	Args: cobra.NoArgs,
	RunE: runDungeonMigrate,
}

func init() {
	Cmd.AddCommand(dungeonMigrateCmd)

	flags := dungeonMigrateCmd.Flags()
	flags.BoolVar(&migrateDryRun, "dry-run", false, "Show what would move without changing anything")
	flags.BoolVar(&migrateNoCommit, "no-commit", false, "Move the directories but do not commit")
}

func runDungeonMigrate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	plan, err := migrate.BuildPlan(ctx, campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "planning dungeon migration")
	}

	if plan.Empty() {
		fmt.Printf("%s Nothing to migrate: this campaign already uses %s\n",
			ui.SuccessIcon(), ui.Value(spelling.Hidden+"/"))
		return nil
	}

	for _, m := range plan.Moves {
		fmt.Printf("  %s -> %s\n",
			ui.Value(relToRoot(campaignRoot, m.From)), ui.Value(relToRoot(campaignRoot, m.To)))
	}

	if migrateDryRun {
		fmt.Printf("\n%s Dry run: %d dungeon(s) would move, nothing changed\n", ui.InfoIcon(), len(plan.Moves))
		return nil
	}

	if err := migrate.Apply(ctx, plan); err != nil {
		return camperrors.Wrap(err, "migrating dungeons")
	}

	fmt.Printf("\n%s Migrated %d dungeon(s) to %s\n",
		ui.SuccessIcon(), len(plan.Moves), ui.Value(spelling.Hidden+"/"))

	if !plan.Git {
		return nil
	}
	if migrateNoCommit {
		fmt.Printf("%s Skipped the commit (--no-commit); review with git status\n", ui.InfoIcon())
		return nil
	}
	if !plan.Committable {
		fmt.Printf("%s This campaign has no commits yet, so there is nothing to commit against\n", ui.InfoIcon())
		return nil
	}

	added, removed := plan.CommitPaths()
	res := commit.DungeonMigrate(ctx, commit.DungeonMigrateOptions{
		Options: commit.Options{
			CampaignRoot: campaignRoot,
			CampaignID:   cfg.ID,
			CampaignName: cfg.Name,
			Files:        added,
			PreStaged:    removed,
		},
		Description: migrationDescription(campaignRoot, plan),
	})
	commit.WarnIfSkipped(os.Stderr, res)
	if res.Err != nil {
		fmt.Printf("\n%s The dungeons were moved on disk, but the commit failed.\n", ui.WarningIcon())
		return camperrors.Wrap(res.Err, "committing dungeon migration")
	}
	fmt.Printf("%s %s\n", ui.InfoIcon(), res.Message)
	return nil
}

func migrationDescription(campaignRoot string, plan *migrate.Plan) string {
	lines := make([]string, 0, len(plan.Moves)+1)
	lines = append(lines, "Converted the campaign to a single hidden dungeon spelling:")
	for _, m := range plan.Moves {
		lines = append(lines, fmt.Sprintf("- %s -> %s",
			relToRoot(campaignRoot, m.From), relToRoot(campaignRoot, m.To)))
	}
	return strings.Join(lines, "\n")
}

func relToRoot(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
