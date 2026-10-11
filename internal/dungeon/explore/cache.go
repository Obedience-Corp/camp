package explore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// cacheSchemaVersion changes whenever the cached index gains information a
// stale cache would silently lack, so old files are rebuilt instead of trusted.
const cacheSchemaVersion = "camp-dungeon-explore-cache/v4"

type cacheFile struct {
	SchemaVersion string `json:"schema_version"`
	Index         Index  `json:"index"`
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
// is missing, unreadable, or was written by a different schema.
func LoadCache(ctx context.Context, dir string) (Index, bool, error) {
	if err := ctx.Err(); err != nil {
		return Index{}, false, camperrors.Wrap(err, "loading dungeon feed cache")
	}
	data, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return Index{}, false, nil
	}
	var file cacheFile
	if err := json.Unmarshal(data, &file); err != nil || file.SchemaVersion != cacheSchemaVersion {
		return Index{}, false, nil
	}
	return file.Index, true, nil
}

// SaveCache writes the index. A failure is returned to the caller, which
// should keep the in-memory feed.
func SaveCache(ctx context.Context, dir string, idx Index) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "saving dungeon feed cache")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return camperrors.Wrap(err, "creating dungeon feed cache")
	}
	data, err := json.Marshal(cacheFile{SchemaVersion: cacheSchemaVersion, Index: idx})
	if err != nil {
		return camperrors.Wrap(err, "encoding dungeon feed cache")
	}
	path := filepath.Join(dir, "index.json")
	// A unique, exclusively created file cannot follow a pre-existing symlink.
	f, err := os.CreateTemp(dir, ".index-*.tmp")
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
	if err := os.Rename(tmp, path); err != nil {
		return camperrors.Wrap(err, "publishing dungeon feed cache")
	}
	return nil
}

// Refresh returns cached when the dungeon set and fingerprints still match.
// Otherwise it builds a new index. changed is true when the returned index
// was just built.
func Refresh(ctx context.Context, root string, cached Index) (Index, bool, error) {
	if err := ctx.Err(); err != nil {
		return Index{}, false, camperrors.Wrap(err, "context cancelled")
	}
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
