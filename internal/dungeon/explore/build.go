package explore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Build walks every dungeon under root and returns the feed index.
// It stats replay files and reads small metadata files. It does not decode GIFs.
func Build(ctx context.Context, root string) (Index, error) {
	if err := ctx.Err(); err != nil {
		return Index{}, camperrors.Wrap(err, "context cancelled")
	}
	found, err := spelling.Discover(ctx, root)
	if err != nil {
		return Index{}, err
	}
	idx := Index{BuiltAt: time.Now().UTC()}
	for _, dungeon := range found {
		if err := ctx.Err(); err != nil {
			return Index{}, camperrors.Wrap(err, "context cancelled")
		}
		print, items, warnings, err := scanDungeon(ctx, root, dungeon)
		if err != nil {
			return Index{}, err
		}
		idx.Dungeons = append(idx.Dungeons, print)
		idx.Items = append(idx.Items, items...)
		idx.Warnings = append(idx.Warnings, warnings...)
	}
	Sort(idx.Items)
	return idx, nil
}

func scanDungeon(ctx context.Context, root string, dungeon spelling.Dungeon) (DungeonPrint, []Item, []string, error) {
	relDungeon, err := relSlash(root, dungeon.Path)
	if err != nil {
		return DungeonPrint{}, nil, nil, err
	}
	parentRel, err := relSlash(root, dungeon.Parent)
	if err != nil {
		return DungeonPrint{}, nil, nil, err
	}
	label := CleanText(Label(parentRel))
	fp, err := fingerprint(ctx, dungeon.Path)
	if err != nil {
		return DungeonPrint{}, nil, []string{warn(relDungeon, err)}, nil
	}
	print := DungeonPrint{Path: relDungeon, Name: dungeon.Name, Label: label, Fingerprint: fp}

	entries, err := os.ReadDir(dungeon.Path)
	if err != nil {
		return print, nil, []string{warn(relDungeon, err)}, nil
	}
	var items []Item
	var warnings []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return DungeonPrint{}, nil, nil, camperrors.Wrap(err, "context cancelled")
		}
		name := entry.Name()
		if skipName(name) || isSymlink(entry) {
			continue
		}
		path := filepath.Join(dungeon.Path, name)
		if !entry.IsDir() {
			item, w := readItem(root, relDungeon, label, StatusHolding, "", path, entry)
			if w != "" {
				warnings = append(warnings, w)
			}
			items = append(items, item)
			continue
		}
		children, w, err := scanStatus(ctx, root, relDungeon, label, name, path)
		if err != nil {
			return DungeonPrint{}, nil, nil, err
		}
		warnings = append(warnings, w...)
		items = append(items, children...)
	}
	return print, items, warnings, nil
}

func scanStatus(ctx context.Context, root, relDungeon, label, status, statusPath string) ([]Item, []string, error) {
	entries, err := os.ReadDir(statusPath)
	if err != nil {
		return nil, []string{warn(relDungeon+"/"+status, err)}, nil
	}
	var items []Item
	var warnings []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, camperrors.Wrap(err, "context cancelled")
		}
		name := entry.Name()
		if skipName(name) || isSymlink(entry) {
			continue
		}
		path := filepath.Join(statusPath, name)
		if entry.IsDir() && statuspath.IsDateDir(name) {
			nested, w, err := scanBucket(ctx, root, relDungeon, label, status, name, path)
			if err != nil {
				return nil, nil, err
			}
			warnings = append(warnings, w...)
			items = append(items, nested...)
			continue
		}
		item, w := readItem(root, relDungeon, label, status, "", path, entry)
		if w != "" {
			warnings = append(warnings, w)
		}
		items = append(items, item)
	}
	return items, warnings, nil
}

func scanBucket(ctx context.Context, root, relDungeon, label, status, day, bucketPath string) ([]Item, []string, error) {
	entries, err := os.ReadDir(bucketPath)
	if err != nil {
		return nil, []string{warn(relDungeon+"/"+status+"/"+day, err)}, nil
	}
	var items []Item
	var warnings []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, camperrors.Wrap(err, "context cancelled")
		}
		name := entry.Name()
		if skipName(name) || isSymlink(entry) {
			continue
		}
		item, w := readItem(root, relDungeon, label, status, day, filepath.Join(bucketPath, name), entry)
		if w != "" {
			warnings = append(warnings, w)
		}
		items = append(items, item)
	}
	return items, warnings, nil
}

func readItem(root, relDungeon, label, status, bucketDay, path string, entry fs.DirEntry) (Item, string) {
	rel, err := relSlash(root, path)
	if err != nil {
		return Item{}, warn(path, err)
	}
	info, err := entry.Info()
	if err != nil {
		return Item{Title: CleanText(entry.Name()), Path: rel, Status: status, DungeonLabel: label, DungeonPath: relDungeon, Kind: KindOther, DateSource: DateFileTime, DoneDate: time.Now().Format("2006-01-02"), Warning: CleanText(err.Error())}, warn(rel, err)
	}
	item := Item{
		Status:       status,
		DungeonLabel: label,
		DungeonPath:  relDungeon,
		Title:        entry.Name(),
		Path:         rel,
		IsDir:        entry.IsDir(),
		Kind:         KindOther,
		DateSource:   DateFileTime,
		DoneDate:     info.ModTime().Format("2006-01-02"),
	}
	if bucketDay != "" {
		item.DoneDate = bucketDay
		item.DateSource = DateBucket
	}
	if entry.IsDir() {
		fillDirectory(&item, path)
	} else if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
		fillMarkdown(&item, path)
	}
	if item.DateSource == DateFileTime {
		item.DoneDate = info.ModTime().Format("2006-01-02")
	}
	item.Title = CleanText(item.Title)
	item.ID = CleanText(item.ID)
	item.Summary = oneLine(CleanText(item.Summary))
	item.Warning = CleanText(item.Warning)
	return item, ""
}

func skipName(name string) bool {
	return name == ".gitkeep" || name == "OBEY.md" || name == "crawl.jsonl" || strings.HasPrefix(name, ".")
}

func isSymlink(entry fs.DirEntry) bool {
	return entry.Type()&os.ModeSymlink != 0
}

func relSlash(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", camperrors.Wrapf(err, "relative path for %s", path)
	}
	return filepath.ToSlash(rel), nil
}

func warn(path string, err error) string {
	return CleanText(path + ": " + err.Error())
}

var itemMetaFiles = []string{"fest.yaml", "FESTIVAL_GOAL.md", ".workitem", "README.md"}

func stamp(rel string, info fs.FileInfo) string {
	return rel + " " + info.ModTime().UTC().Format(time.RFC3339Nano) + " " + strconv.FormatInt(info.Size(), 10)
}

func fingerprint(ctx context.Context, dungeonPath string) (string, error) {
	var lines []string
	err := filepath.WalkDir(dungeonPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == dungeonPath {
			return nil
		}
		rel, relErr := filepath.Rel(dungeonPath, path)
		if relErr != nil {
			return relErr
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		lines = append(lines, stamp(filepath.ToSlash(rel), info))
		// Status directories and YYYY-MM-DD buckets are containers. Everything
		// else is an item. A directory's mtime misses in-place edits, so the
		// metadata files an item is read from are stamped as well.
		if entry.IsDir() && !statuspath.IsDateDir(entry.Name()) && strings.Count(filepath.ToSlash(rel), "/") >= 1 {
			for _, name := range itemMetaFiles {
				meta, err := os.Lstat(filepath.Join(path, name))
				if err != nil {
					continue
				}
				lines = append(lines, stamp(filepath.ToSlash(rel)+"/"+name, meta))
			}
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:]), nil
}
