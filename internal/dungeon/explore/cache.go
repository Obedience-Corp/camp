package explore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// cacheSchemaVersion changes whenever the cached index gains information a
// stale cache would silently lack, so old files are rebuilt instead of trusted.
const cacheSchemaVersion = "camp-dungeon-explore-cache/v7"

const (
	cacheFileName    = "index.json"
	cacheTempPattern = ".index-*.tmp"
	// staleTempAge is longer than any save takes, so a file this old belongs
	// to a process that exited before it could publish or remove it.
	staleTempAge = time.Minute
)

type cacheFile struct {
	SchemaVersion string `json:"schema_version"`
	// Zone is the time zone the index was built in. A row dated by its file
	// time holds that zone's calendar day, so another zone reads it again.
	Zone  string `json:"zone"`
	Index Index  `json:"index"`
}

// zoneKey names the local time zone by its offsets in January and in July.
// The pair tells apart two zones that agree today and differ across the
// year, and it does not change when the clocks do.
func zoneKey() string {
	year := time.Now().Year()
	_, winter := time.Date(year, time.January, 1, 12, 0, 0, 0, time.Local).Zone()
	_, summer := time.Date(year, time.July, 1, 12, 0, 0, 0, time.Local).Zone()
	return strconv.Itoa(winter) + "/" + strconv.Itoa(summer)
}

// CacheDir is the user-level directory for one checkout's feed cache. It sits
// under the camp config directory, so XDG_CONFIG_HOME moves it with the rest.
func CacheDir(campaignID, root string) (string, error) {
	base, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	// Repository-controlled IDs are opaque identifiers, never path components.
	// Two checkouts of one camp share an ID, so the root is part of the key:
	// neither may open on the other's rows.
	sum := sha256.Sum256([]byte("id:" + campaignID + "\x00root:" + filepath.Clean(root)))
	return filepath.Join(base, "caches", hex.EncodeToString(sum[:]), "dungeon-feed"), nil
}

// LoadCache reads a previously saved index. The bool is false when the file
// is missing, unreadable, or was written by a different schema or in a
// different time zone.
func LoadCache(ctx context.Context, dir string) (Index, bool, error) {
	if err := ctx.Err(); err != nil {
		return Index{}, false, camperrors.Wrap(err, "loading dungeon feed cache")
	}
	data, err := os.ReadFile(filepath.Join(dir, cacheFileName))
	if err != nil {
		return Index{}, false, nil
	}
	var file cacheFile
	if err := json.Unmarshal(data, &file); err != nil || file.SchemaVersion != cacheSchemaVersion || file.Zone != zoneKey() {
		return Index{}, false, nil
	}
	return file.Index, true, nil
}

// SaveCache writes the index. A failure is returned to the caller, which
// should keep the in-memory feed.
//
// An index JSON cannot carry unchanged is not written, and a cache already
// there is removed: it describes an earlier camp, and the next start reads
// the camp instead of painting from it.
func SaveCache(ctx context.Context, dir string, idx Index) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "saving dungeon feed cache")
	}
	if !survivesJSON(idx) {
		if err := os.Remove(filepath.Join(dir, cacheFileName)); err != nil && !camperrors.Is(err, fs.ErrNotExist) {
			return camperrors.Wrap(err, "removing dungeon feed cache")
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return camperrors.Wrap(err, "creating dungeon feed cache")
	}
	data, err := json.Marshal(cacheFile{SchemaVersion: cacheSchemaVersion, Zone: zoneKey(), Index: idx})
	if err != nil {
		return camperrors.Wrap(err, "encoding dungeon feed cache")
	}
	path := filepath.Join(dir, cacheFileName)
	sweepStaleTemps(dir)
	// A unique, exclusively created file cannot follow a pre-existing symlink.
	f, err := os.CreateTemp(dir, cacheTempPattern)
	if err != nil {
		return camperrors.Wrap(err, "creating dungeon feed cache file")
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return camperrors.Wrap(err, "writing dungeon feed cache")
	}
	if err := f.Close(); err != nil {
		return camperrors.Wrap(err, "closing dungeon feed cache")
	}
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "saving dungeon feed cache")
	}
	if err := os.Rename(tmp, path); err != nil {
		return camperrors.Wrap(err, "publishing dungeon feed cache")
	}
	return nil
}

// survivesJSON reports whether the index reads back from the cache as it was
// written. JSON text is UTF-8: a file or directory name in another encoding
// is written with U+FFFD in place of each byte that is not, and comes back
// as a path that is not on disk. The fingerprints still match, so such a
// cache would be trusted on every start. Titles, ids, summaries, labels, and
// warnings are cleaned text and always valid.
func survivesJSON(idx Index) bool {
	for _, d := range idx.Dungeons {
		if !utf8.ValidString(d.Path) {
			return false
		}
	}
	for _, item := range idx.Items {
		for _, name := range []string{item.Path, item.Replay, item.Status, item.DungeonPath} {
			if !utf8.ValidString(name) {
				return false
			}
		}
	}
	return true
}

// sweepStaleTemps removes temp files an earlier save left behind. A save that
// returns removes its own; one whose process exits partway cannot.
func sweepStaleTemps(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if match, _ := filepath.Match(cacheTempPattern, entry.Name()); !match || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < staleTempAge {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// Refresh returns cached when the dungeon set and fingerprints still match.
// Otherwise it builds a new index. changed is true when the returned index
// was just built.
func Refresh(ctx context.Context, root string, cached Index) (Index, bool, error) {
	if err := ctx.Err(); err != nil {
		return Index{}, false, camperrors.Wrap(err, "context cancelled")
	}
	root = resolveRoot(root)
	found, err := spelling.DiscoverContinuing(ctx, root, walkDir)
	if err != nil {
		return Index{}, false, err
	}
	// An unreadable directory or a failed metadata read does not change
	// dungeon fingerprints, so a matching cache would keep the stale warning.
	// Rebuild to retry it.
	if len(found.Warnings) == 0 && !cachedHasWarning(cached) && fingerprintsMatch(ctx, root, cached, found.Dungeons) {
		return cached, false, nil
	}
	idx, err := Build(ctx, root)
	return idx, true, err
}

func cachedHasWarning(idx Index) bool {
	if len(idx.Warnings) > 0 {
		return true
	}
	for _, item := range idx.Items {
		if item.Warning != "" {
			return true
		}
	}
	return false
}

func fingerprintsMatch(ctx context.Context, root string, cached Index, found []spelling.Dungeon) bool {
	if len(found) != len(cached.Dungeons) {
		return false
	}
	byPath := make(map[string]string, len(cached.Dungeons))
	for _, d := range cached.Dungeons {
		byPath[d.Path] = d.Fingerprint
	}
	for _, dungeon := range found {
		rel, err := relSlash(root, dungeon.Path)
		if err != nil {
			return false
		}
		want, ok := byPath[rel]
		if !ok {
			return false
		}
		fp, err := fingerprint(ctx, dungeon.Path)
		if err != nil || fp != want {
			return false
		}
	}
	return true
}
