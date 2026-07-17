package spelling

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ProjectsDir is the campaign-root directory holding project checkouts. The
// dungeon sweep never descends into it.
const ProjectsDir = "projects"

// Dungeon is one dungeon directory found under a campaign.
type Dungeon struct {
	// Parent is the directory holding the dungeon.
	Parent string
	// Name is "dungeon" or ".dungeon".
	Name string
	// Path is filepath.Join(Parent, Name).
	Path string
}

// Hidden reports whether d uses the hidden spelling.
func (d Dungeon) Hidden() bool { return d.Name == Hidden }

// Discover walks campaignRoot and reports every dungeon directory it holds,
// ordered by path.
//
// A campaign carries roughly a dozen dungeons (the root, festivals/,
// .campaign/intents/, .campaign/quests/, and one per workflow type), and they
// nest: an archived work item can carry a dungeon of its own. The walk
// therefore descends through a dungeon it has already recorded rather than
// stopping at it.
//
// projects/ is skipped whole. Projects own their own trees, and a Go package
// directory named "dungeon" inside one is not a campaign dungeon. Nested git
// repositories are skipped for the same reason.
func Discover(ctx context.Context, campaignRoot string) ([]Dungeon, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	absRoot, err := filepath.Abs(campaignRoot)
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolving campaign root %s", campaignRoot)
	}

	// A directory that does not exist yet holds no dungeons. camp init resolves
	// the campaign's spelling before the scaffold step creates the directory,
	// so this is the normal path for a brand-new campaign, not an error.
	info, err := os.Stat(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, camperrors.Wrapf(err, "stat %s", absRoot)
	}
	if !info.IsDir() {
		return nil, nil
	}

	projectsPath := filepath.Join(absRoot, ProjectsDir)

	var found []Dungeon
	walkErr := filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() || path == absRoot {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Name() == gitDirName || path == projectsPath || isNestedRepo(path) {
			return filepath.SkipDir
		}
		if IsDungeonName(entry.Name()) {
			found = append(found, Dungeon{Parent: filepath.Dir(path), Name: entry.Name(), Path: path})
		}
		return nil
	})
	if walkErr != nil {
		return nil, camperrors.Wrapf(walkErr, "walking campaign %s", absRoot)
	}

	sort.Slice(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	return found, nil
}

// CampaignName reports the dungeon spelling campaignRoot has established.
//
// The root dungeon is authoritative: camp init always scaffolds one, and
// camp dungeon migrate converts every dungeon in the campaign in a single
// commit, so a campaign is legacy exactly while its root dungeon is visible.
// A campaign whose root dungeon is missing falls back to a full sweep, and a
// campaign with no dungeon anywhere (a brand-new one) falls back to
// hiddenDefault, the dungeon_hidden setting.
func CampaignName(ctx context.Context, campaignRoot string, hiddenDefault bool) (string, error) {
	rootResolved, err := Resolve(ctx, campaignRoot)
	if err != nil {
		return "", err
	}
	if rootResolved.Exists {
		return rootResolved.Name, nil
	}

	found, err := Discover(ctx, campaignRoot)
	if err != nil {
		return "", err
	}
	for _, d := range found {
		if !d.Hidden() {
			return Visible, nil
		}
	}
	if len(found) > 0 {
		return Hidden, nil
	}
	return NameFor(hiddenDefault), nil
}

const gitDirName = ".git"

func isNestedRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, gitDirName))
	return err == nil
}
