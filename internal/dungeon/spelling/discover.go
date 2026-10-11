package spelling

import (
	"context"
	"errors"
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

// Discovery is the dungeons under a campaign and the directories the walk
// could not list.
type Discovery struct {
	Dungeons []Dungeon
	// Warnings are camp-relative paths of directories whose contents could
	// not be read, each followed by the error text.
	Warnings []string
}

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
//
// A directory that cannot be listed fails the whole walk. Callers that can
// keep the dungeons they did reach want DiscoverContinuing.
func Discover(ctx context.Context, campaignRoot string) ([]Dungeon, error) {
	found, err := discover(ctx, campaignRoot, false, filepath.WalkDir)
	return found.Dungeons, err
}

// DiscoverContinuing walks campaignRoot like Discover. A directory that
// cannot be listed is recorded in Warnings and the walk continues, so one
// unreadable directory does not hide every other dungeon. Failure to read
// campaignRoot itself, or a cancelled context, is still an error.
//
// walk lists the tree the way filepath.WalkDir does. A nil walk uses
// filepath.WalkDir.
func DiscoverContinuing(ctx context.Context, campaignRoot string, walk func(string, fs.WalkDirFunc) error) (Discovery, error) {
	if walk == nil {
		walk = filepath.WalkDir
	}
	return discover(ctx, campaignRoot, true, walk)
}

func discover(ctx context.Context, campaignRoot string, tolerate bool, walk func(string, fs.WalkDirFunc) error) (Discovery, error) {
	if err := ctx.Err(); err != nil {
		return Discovery{}, camperrors.Wrap(err, "context cancelled")
	}

	absRoot, err := filepath.Abs(campaignRoot)
	if err != nil {
		return Discovery{}, camperrors.Wrapf(err, "resolving camp root %s", campaignRoot)
	}

	// A directory that does not exist yet holds no dungeons. camp init resolves
	// the campaign's spelling before the scaffold step creates the directory,
	// so this is the normal path for a brand-new campaign, not an error.
	info, err := os.Stat(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return Discovery{}, nil
		}
		return Discovery{}, camperrors.Wrapf(err, "stat %s", absRoot)
	}
	if !info.IsDir() {
		return Discovery{}, nil
	}

	projectsPath := filepath.Join(absRoot, ProjectsDir)

	var found Discovery
	walkErr := walk(absRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if !tolerate || path == absRoot {
				return err
			}
			found.Warnings = append(found.Warnings, discoveryWarning(absRoot, path, err))
			return nil
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
			found.Dungeons = append(found.Dungeons, Dungeon{Parent: filepath.Dir(path), Name: entry.Name(), Path: path})
		}
		return nil
	})
	if walkErr != nil {
		return Discovery{}, camperrors.Wrapf(walkErr, "walking camp %s", absRoot)
	}

	sort.Slice(found.Dungeons, func(i, j int) bool { return found.Dungeons[i].Path < found.Dungeons[j].Path })
	return found, nil
}

func discoveryWarning(root, path string, err error) string {
	rel, relErr := filepath.Rel(root, path)
	if relErr != nil || rel == "." {
		rel = path
	}
	// The path error repeats the absolute path the warning already names.
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	return filepath.ToSlash(rel) + ": " + err.Error()
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
