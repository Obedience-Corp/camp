package explore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// walkDir lists the camp and fingerprints each dungeon. Tests replace it to
// report a directory the process cannot read.
var walkDir = filepath.WalkDir

// Build walks every dungeon under root and returns the feed index.
// It stats replay files and reads small metadata files. It does not decode GIFs.
func Build(ctx context.Context, root string) (Index, error) {
	if err := ctx.Err(); err != nil {
		return Index{}, camperrors.Wrap(err, "context cancelled")
	}
	found, err := spelling.DiscoverContinuing(ctx, root, walkDir)
	if err != nil {
		return Index{}, err
	}
	idx := Index{BuiltAt: time.Now().UTC(), Warnings: addWarnings(nil, found.Warnings...)}
	for _, dungeon := range found.Dungeons {
		if err := ctx.Err(); err != nil {
			return Index{}, camperrors.Wrap(err, "context cancelled")
		}
		print, items, warnings, err := scanDungeon(ctx, root, dungeon)
		if err != nil {
			return Index{}, err
		}
		idx.Dungeons = append(idx.Dungeons, print)
		idx.Items = append(idx.Items, items...)
		idx.Warnings = addWarnings(idx.Warnings, warnings...)
	}
	if err := ctx.Err(); err != nil {
		return Index{}, camperrors.Wrap(err, "context cancelled")
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
	var warnings []string
	if err != nil {
		if ctx.Err() != nil {
			return DungeonPrint{}, nil, nil, camperrors.Wrap(err, "context cancelled")
		}
		warnings = append(warnings, warn(root, relDungeon, err))
		fp = ""
	}
	print := DungeonPrint{Path: relDungeon, Name: dungeon.Name, Label: label, Fingerprint: fp}

	entries, err := os.ReadDir(dungeon.Path)
	if err != nil {
		return print, nil, append(warnings, warn(root, relDungeon, err)), nil
	}
	var items []Item
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return DungeonPrint{}, nil, nil, camperrors.Wrap(err, "context cancelled")
		}
		name := entry.Name()
		if skipName(name) || isSymlink(entry) {
			continue
		}
		path := filepath.Join(dungeon.Path, name)
		if !entry.IsDir() || markedItemDirectory(path) {
			item, w := readItem(ctx, root, relDungeon, label, StatusHolding, "", path, entry)
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
		return nil, []string{warn(root, relDungeon+"/"+status, err)}, nil
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
		item, w := readItem(ctx, root, relDungeon, label, status, "", path, entry)
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
		return nil, []string{warn(root, relDungeon+"/"+status+"/"+day, err)}, nil
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
		item, w := readItem(ctx, root, relDungeon, label, status, day, filepath.Join(bucketPath, name), entry)
		if w != "" {
			warnings = append(warnings, w)
		}
		items = append(items, item)
	}
	return items, warnings, nil
}

func readItem(ctx context.Context, root, relDungeon, label, status, bucketDay, path string, entry fs.DirEntry) (Item, string) {
	rel, err := relSlash(root, path)
	if err != nil {
		return Item{}, warn(root, entry.Name(), err)
	}
	info, err := entry.Info()
	if err != nil {
		item := Item{Title: entry.Name(), Path: rel, Status: status, DungeonLabel: label, DungeonPath: relDungeon, Kind: KindOther, DateSource: DateFileTime, DoneDate: time.Now().Format("2006-01-02"), Warning: cause(err).Error()}
		tidy(&item, entry.Name())
		return item, warn(root, rel, err)
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
		fillDirectory(ctx, &item, path)
	} else if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
		fillMarkdown(ctx, &item, path)
	}
	if item.DateSource == DateFileTime {
		item.DoneDate = info.ModTime().Format("2006-01-02")
	}
	tidy(&item, entry.Name())
	return item, ""
}

// tidy cleans and bounds the text a row took from the camp. A title with
// nothing left to show falls back to the file or directory name.
func tidy(item *Item, name string) {
	item.Title = CleanText(item.Title)
	if strings.TrimSpace(item.Title) == "" {
		item.Title = CleanText(name)
	}
	item.Title = capRunes(item.Title, titleLimit)
	item.ID = capRunes(CleanText(item.ID), idLimit)
	item.Summary = oneLine(CleanText(item.Summary))
	item.Warning = capRunes(CleanText(item.Warning), warningLimit)
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

// warn names what could not be read by its camp-relative path. A filesystem
// error names the absolute path it failed on, which may be below fallback;
// the warning carries that path once, relative to the camp.
func warn(root, fallback string, err error) string {
	rel := fallback
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		if under, relErr := filepath.Rel(root, pathErr.Path); relErr == nil && filepath.IsLocal(under) {
			rel = filepath.ToSlash(under)
		}
	}
	return CleanText(rel + ": " + cause(err).Error())
}

// cause is err without the absolute path a filesystem error repeats.
func cause(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

// addWarnings appends each warning the list does not hold yet. Discovery, the
// fingerprint walk, and the item scan all stop at the same unreadable
// directory, and it is reported once. The list stops at maxIndexWarnings.
func addWarnings(have []string, more ...string) []string {
	for _, warning := range more {
		if len(have) >= maxIndexWarnings {
			break
		}
		warning = capRunes(CleanText(warning), warningLimit)
		if warning == "" || slices.Contains(have, warning) {
			continue
		}
		have = append(have, warning)
	}
	return have
}

var itemMetaFiles = []string{"fest.yaml", "FESTIVAL_GOAL.md", ".workitem", "README.md"}

// stamp carries the mode because a chmod leaves size and modification time
// alone and still decides whether the file can be read.
func stamp(rel string, info fs.FileInfo) string {
	return rel + " " + info.Mode().String() + " " + info.ModTime().UTC().Format(time.RFC3339Nano) + " " + strconv.FormatInt(info.Size(), 10)
}

func fingerprint(ctx context.Context, dungeonPath string) (string, error) {
	var lines []string
	err := walkDir(dungeonPath, func(path string, entry fs.DirEntry, err error) error {
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
		if entry.IsDir() && !statuspath.IsDateDir(entry.Name()) && (strings.Count(filepath.ToSlash(rel), "/") >= 1 || markedItemDirectory(path)) {
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

// Explicit item metadata takes precedence over the legacy directory-as-status
// layout, including items moved directly into holding by dungeon move --triage.
func markedItemDirectory(path string) bool {
	return fileExists(filepath.Join(path, ".workitem")) ||
		fileExists(filepath.Join(path, "fest.yaml")) ||
		fileExists(filepath.Join(path, "FESTIVAL_GOAL.md")) ||
		replayPath(path, filepath.Base(path)) != ""
}
