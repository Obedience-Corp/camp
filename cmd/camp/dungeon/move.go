package dungeon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var dungeonMoveCmd = &cobra.Command{
	Use:   "move <item>... [status]",
	Short: "Move dungeon items between statuses",
	Long: `Move items within the dungeon or from the parent directory into the dungeon.

By default, moves an item already in the dungeon root to a status directory.
When the item exists in the parent directory and not in the dungeon root, the
command automatically treats it as triage work and moves it into the dungeon.
Use --triage to force a parent-directory move.
With --triage and --to-docs, routes items to an existing campaign-root docs/<subdirectory>.
With --workitem, resolves a campaign workitem from anywhere and moves its directory
into the workitem type's local dungeon.
Moves are always auto-committed so dungeon history remains auditable.

Statuses: completed, archived, someday

Batch: pass several items followed by one shared status to move them together
(default, --triage, and --to-docs modes). Every item is validated before any
move is applied, so an invalid item aborts the whole sweep. --workitem accepts a
single item per invocation.

Dry run: --dry-run resolves and validates exactly as a real move would, prints
the source -> destination for each item, and exits without touching the
filesystem or creating a commit. Add --json for a machine-readable plan.

Examples:
  camp dungeon move old-feature archived         Move dungeon item to archived
  camp dungeon move stale-doc completed          Move dungeon item to completed
  camp dungeon move a b c archived               Move three items to archived (batch)
  camp dungeon move a b c completed --dry-run    Preview the sweep, change nothing
  camp dungeon move old-project --triage         Move parent item into dungeon root
  camp dungeon move old-project archived --triage Move parent item directly to archived
  camp dungeon move stale-note.md --triage --to-docs architecture/api Route to docs subdirectory
  camp dungeon move feature-slug archived --workitem Move workitem directory to its local archive`,
	Args: cobra.MinimumNArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive move of dungeon items between statuses",
	},
	RunE: runDungeonMove,
}

func init() {
	Cmd.AddCommand(dungeonMoveCmd)

	flags := dungeonMoveCmd.Flags()
	flags.Bool("triage", false, "Move from parent directory (not from dungeon root)")
	flags.String("to-docs", "", "Route triage item into an existing campaign-root docs/<subdir> (requires --triage)")
	flags.Bool("workitem", false, "Resolve item as a campaign workitem and move its directory to the local dungeon")
	flags.Bool("dry-run", false, "Preview the move(s) without touching the filesystem or creating a commit")
	flags.Bool("json", false, "Emit the dry-run plan as JSON (requires --dry-run)")
}

func runDungeonMove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	f := moveFlags{}
	f.triage, _ = cmd.Flags().GetBool("triage")
	f.toDocs, _ = cmd.Flags().GetString("to-docs")
	f.workitem, _ = cmd.Flags().GetBool("workitem")
	f.dryRun, _ = cmd.Flags().GetBool("dry-run")
	f.jsonOut, _ = cmd.Flags().GetBool("json")

	items, status, err := resolveItemsAndStatus(args, f)
	if err != nil {
		return err
	}
	if err := validateMoveModes(f, items); err != nil {
		return err
	}

	if f.workitem {
		return runWorkitemMove(ctx, cmd, items[0], status, f)
	}
	return runDungeonItemsMove(ctx, items, status, f)
}

func inferDungeonMoveTriageMode(ctx context.Context, svc *intdungeon.Service, dungeonCtx intdungeon.Context, itemName string) (bool, error) {
	cleanItem, ok := dungeonMoveDirectChildName(itemName)
	if !ok {
		return false, nil
	}

	dungeonRootExists := false
	if _, err := os.Stat(filepath.Join(dungeonCtx.DungeonPath, cleanItem)); err == nil {
		dungeonRootExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, camperrors.Wrapf(err, "checking dungeon item %s", cleanItem)
	}

	parentEligible := false
	items, err := svc.ListParentItems(ctx, dungeonCtx.ParentPath)
	if err != nil {
		return false, camperrors.Wrap(err, "checking parent triage items")
	}
	for _, item := range items {
		if item.Name == cleanItem {
			parentEligible = true
			break
		}
	}
	return shouldInferDungeonMoveTriageMode(cleanItem, dungeonRootExists, parentEligible), nil
}

func shouldInferDungeonMoveTriageMode(itemName string, dungeonRootExists, parentEligible bool) bool {
	if _, ok := dungeonMoveDirectChildName(itemName); !ok {
		return false
	}
	return !dungeonRootExists && parentEligible
}

func dungeonMoveDirectChildName(itemName string) (string, bool) {
	cleanItem := strings.TrimSpace(itemName)
	if cleanItem == "" || cleanItem != itemName || filepath.Clean(cleanItem) != cleanItem || filepath.Base(cleanItem) != cleanItem {
		return "", false
	}
	if strings.Contains(cleanItem, "/") || strings.Contains(cleanItem, "\\") {
		return "", false
	}
	return cleanItem, true
}

type DungeonMoveCommitOutcome struct {
	StagingErr error
	Committed  bool
	NoChanges  bool
	Message    string
	CommitErr  error
}

func StageAndCommitDungeonMove(ctx context.Context, move *DungeonMoveCommit) *DungeonMoveCommitOutcome {
	outcome := &DungeonMoveCommitOutcome{}
	files := commit.NormalizeFiles(move.CampaignRoot, move.DestinationPaths...)
	files = append(files, commit.NormalizeFiles(move.CampaignRoot, move.RewrittenFiles...)...)
	preStaged, err := StageTrackedMoveSourceDeletions(ctx, move.CampaignRoot, move.SourcePaths)
	if err != nil {
		outcome.StagingErr = err
		return outcome
	}
	result := commit.Crawl(ctx, commit.CrawlOptions{
		Options: commit.Options{
			CampaignRoot: move.CampaignRoot,
			CampaignID:   move.Config.ID,
			PreStaged:    preStaged,
		},
		Description: strings.TrimSpace(move.Description),
		Files:       files,
	})
	outcome.Committed = result.Committed
	outcome.NoChanges = result.NoChanges
	outcome.Message = result.Message
	outcome.CommitErr = result.Err
	return outcome
}

func PrintDungeonMoveOutcome(w io.Writer, outcome *DungeonMoveCommitOutcome) {
	switch {
	case outcome.StagingErr != nil:
		fmt.Fprintf(w, "%s Move was applied on disk, but staging the source deletion failed.\n", ui.WarningIcon())
		fmt.Fprintf(w, "%s %v\n", ui.WarningIcon(), outcome.StagingErr)
	case outcome.Committed:
		fmt.Fprintf(w, "%s %s\n", ui.SuccessIcon(), outcome.Message)
	case outcome.CommitErr != nil:
		fmt.Fprintf(w, "%s Move was applied on disk, but auto-commit failed.\n", ui.WarningIcon())
		fmt.Fprintf(w, "%s %v\n", ui.WarningIcon(), outcome.CommitErr)
	case outcome.NoChanges:
		fmt.Fprintf(w, "%s %s\n", ui.InfoIcon(), outcome.Message)
	case outcome.Message != "":
		fmt.Fprintf(w, "%s %s\n", ui.InfoIcon(), outcome.Message)
	}
}

func (o *DungeonMoveCommitOutcome) Err() error {
	if o.StagingErr != nil {
		return camperrors.Wrap(o.StagingErr, "staging move source deletions")
	}
	if o.CommitErr != nil {
		return camperrors.Wrap(o.CommitErr, "auto-committing dungeon move")
	}
	return nil
}

func CommitDungeonMove(ctx context.Context, move *DungeonMoveCommit) error {
	outcome := StageAndCommitDungeonMove(ctx, move)
	PrintDungeonMoveOutcome(os.Stdout, outcome)
	return outcome.Err()
}

// StageTrackedMoveSourceDeletions pre-stages the deletion of tracked source
// paths so a selective commit records a directory move as a rename instead of
// leaving stale deletions unstaged. Returns the campaign-relative paths that
// were staged; untracked sources are skipped.
func StageTrackedMoveSourceDeletions(ctx context.Context, campaignRoot string, sourcePaths []string) ([]string, error) {
	sources := commit.NormalizeFiles(campaignRoot, sourcePaths...)
	if len(sources) == 0 {
		return nil, nil
	}
	tracked, err := git.FilterTracked(ctx, campaignRoot, sources)
	if err != nil {
		return nil, err
	}
	if len(tracked) == 0 {
		return nil, nil
	}
	if err := git.StageTrackedChanges(ctx, campaignRoot, tracked...); err != nil {
		return nil, err
	}
	return tracked, nil
}

func WrapDungeonMoveError(err error, itemName, status string) error {
	switch {
	case errors.Is(err, intdungeon.ErrAlreadyExists):
		return camperrors.Newf(
			"cannot move %q to %q because destination already exists; choose another status or rename the item: %w",
			itemName,
			status,
			err,
		)
	case errors.Is(err, intdungeon.ErrInvalidStatus):
		return camperrors.Newf(
			"invalid status %q for %q; use a single directory name like completed, archived, or someday: %w",
			status,
			itemName,
			err,
		)
	case errors.Is(err, intdungeon.ErrInvalidItemPath):
		return camperrors.Newf(
			"invalid item path %q; use a direct child file or directory name from the current dungeon context (no slashes or traversal). Run 'camp dungeon list --triage' or 'camp dungeon list' to confirm available items: %w",
			itemName,
			err,
		)
	case errors.Is(err, intdungeon.ErrNotFound):
		return camperrors.Newf(
			"item %q was not found in the resolved context; run 'camp dungeon list --triage' or 'camp dungeon list' to confirm available items: %w",
			itemName,
			err,
		)
	default:
		return camperrors.Wrapf(err, "moving %s to %s", itemName, status)
	}
}

func wrapDungeonDocsRouteError(err error, itemName, destination string) error {
	switch {
	case errors.Is(err, intdungeon.ErrAlreadyExists):
		return camperrors.Newf(
			"cannot route %q to docs/%s because destination already exists; choose another docs destination or rename the item: %w",
			itemName,
			destination,
			err,
		)
	case errors.Is(err, intdungeon.ErrInvalidDocsDestination):
		return camperrors.Newf(
			"invalid docs destination %q; use an existing subdirectory under campaign-root docs/ (for example: --to-docs architecture/api): %w",
			destination,
			err,
		)
	case errors.Is(err, intdungeon.ErrInvalidItemPath):
		return camperrors.Newf(
			"invalid item path %q; use a direct child file or directory name from the resolved triage context (no slashes or traversal). Run 'camp dungeon list --triage' to confirm available items: %w",
			itemName,
			err,
		)
	case errors.Is(err, intdungeon.ErrNotFound):
		return camperrors.Newf(
			"item %q was not found in the resolved triage context; run 'camp dungeon list --triage' to confirm available items: %w",
			itemName,
			err,
		)
	default:
		return camperrors.Wrapf(err, "routing %s to docs/%s", itemName, destination)
	}
}
