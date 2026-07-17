package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// dungeonScaffoldPlan captures how the campaign's dungeons looked before the
// guild-scaffold template step ran, plus the spelling they should end up with.
//
// The template step has no notion of hidden dungeons and always mirrors its
// static tree under the visible name, so init cannot ask disk what was there
// once it has run. Everything it needs to know is snapshotted here first.
type dungeonScaffoldPlan struct {
	// parents are the standard dungeon locations the template step scaffolds.
	parents []string
	// preResolved[i] is how parents[i] looked before the template step.
	preResolved []spelling.Resolved
	// intentsParent is the canonical intents root. Its dungeon is reconciled
	// alongside the standard locations even though intent.EnsureDirectories
	// populates it later, so that step sees the right spelling when it runs.
	intentsParent string
	// intentsPre is how intentsParent looked before the template step.
	intentsPre spelling.Resolved
	// campaignSpelling is the spelling this campaign uses. For a new campaign
	// that is the dungeon_hidden setting; for an existing one it is whatever
	// the campaign already established, so a repair never introduces the
	// second spelling that camp dungeon migrate exists to clean up.
	campaignSpelling string
}

// planDungeonScaffold snapshots the campaign's dungeon layout at absDir. It
// must run before the template step.
func planDungeonScaffold(ctx context.Context, absDir string) (*dungeonScaffoldPlan, error) {
	globalCfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to load global config")
	}
	campaignSpelling, err := spelling.CampaignName(ctx, absDir, globalCfg.ResolveDungeonHidden())
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolving dungeon spelling for %s", absDir)
	}

	plan := &dungeonScaffoldPlan{
		parents: []string{
			absDir,
			filepath.Join(absDir, "workflow", "reviews"),
			filepath.Join(absDir, "workflow", "design"),
			filepath.Join(absDir, "workflow", "explore"),
		},
		intentsParent:    filepath.Join(absDir, config.CampaignDir, "intents"),
		campaignSpelling: campaignSpelling,
	}

	plan.preResolved = make([]spelling.Resolved, len(plan.parents))
	for i, parent := range plan.parents {
		resolved, err := spelling.Resolve(ctx, parent)
		if err != nil {
			return nil, camperrors.Wrapf(err, "resolving dungeon spelling under %s", parent)
		}
		plan.preResolved[i] = resolved
	}

	plan.intentsPre, err = spelling.Resolve(ctx, plan.intentsParent)
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolving dungeon spelling under %s", plan.intentsParent)
	}
	return plan, nil
}

// reconcileDungeonSpelling decides which name a dungeon directory under parent
// should use, given how it looked (preResolved) before the template step ran.
// The template step always creates the visible skeleton, so:
//   - if only the hidden spelling pre-existed, the visible skeleton the
//     template step just recreated is a duplicate and is removed;
//   - if the visible spelling pre-existed, it is left untouched;
//   - if neither pre-existed, the freshly scaffolded visible dungeon is
//     renamed when campaignSpelling is hidden.
//
// It returns the final path to use and whether the directory is being
// initialized for the first time (i.e. should be force-populated).
func reconcileDungeonSpelling(result *InitResult, parent string, preResolved spelling.Resolved, campaignSpelling string) (dungeonPath string, isNew bool, err error) {
	visiblePath := filepath.Join(parent, spelling.Visible)
	hiddenPath := filepath.Join(parent, spelling.Hidden)

	switch {
	case preResolved.Exists && preResolved.Hidden():
		if err := os.RemoveAll(visiblePath); err != nil {
			return "", false, camperrors.Wrapf(err, "removing duplicate visible dungeon scaffold at %s", visiblePath)
		}
		result.DirsCreated = removePathsUnder(result.DirsCreated, visiblePath)
		result.FilesCreated = removePathsUnder(result.FilesCreated, visiblePath)
		result.Skipped = removePathsUnder(result.Skipped, visiblePath)
		return hiddenPath, false, nil
	case preResolved.Exists:
		return visiblePath, false, nil
	case campaignSpelling == spelling.Hidden:
		if err := os.Rename(visiblePath, hiddenPath); err != nil {
			return "", false, camperrors.Wrapf(err, "hiding new dungeon scaffold at %s", visiblePath)
		}
		result.DirsCreated = renamePathsUnder(result.DirsCreated, visiblePath, hiddenPath)
		result.FilesCreated = renamePathsUnder(result.FilesCreated, visiblePath, hiddenPath)
		return hiddenPath, true, nil
	default:
		return visiblePath, true, nil
	}
}

// renamePathsUnder rewrites entries in paths that equal oldRoot or live
// nested under it to the equivalent path under newRoot, leaving unrelated
// entries untouched. It mirrors an on-disk directory rename so init
// reporting stays accurate after a standard dungeon path is relocated.
func renamePathsUnder(paths []string, oldRoot, newRoot string) []string {
	if len(paths) == 0 {
		return paths
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		if p == oldRoot {
			out[i] = newRoot
		} else if rel, ok := relUnder(p, oldRoot); ok {
			out[i] = filepath.Join(newRoot, rel)
		} else {
			out[i] = p
		}
	}
	return out
}

// removePathsUnder drops entries in paths equal to root or nested under it.
func removePathsUnder(paths []string, root string) []string {
	if len(paths) == 0 {
		return paths
	}
	out := paths[:0:0]
	for _, p := range paths {
		if p == root {
			continue
		}
		if _, ok := relUnder(p, root); ok {
			continue
		}
		out = append(out, p)
	}
	return out
}

func relUnder(path, root string) (string, bool) {
	prefix := root + string(filepath.Separator)
	if rest, ok := strings.CutPrefix(path, prefix); ok {
		return rest, true
	}
	return "", false
}
