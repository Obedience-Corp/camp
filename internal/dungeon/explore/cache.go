package explore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

type cacheFile struct {
	SchemaVersion string `json:"schema_version"`
	Index         Index  `json:"index"`
}

// CacheDir is the user-level directory for this camp's feed cache.
func CacheDir(campaignID, root string) (string, error) {
	home, err := pathutil.Home()
	if err != nil {
		return "", err
	}
	id := campaignID
	if id == "" {
		sum := sha256.Sum256([]byte(root))
		id = hex.EncodeToString(sum[:8])
	}
	return filepath.Join(home, ".obey", "campaign", "caches", id, "dungeon-feed"), nil
}

// LoadCache reads a previously saved index. The bool is false when the file
// is missing or was written by a different schema.
func LoadCache(dir string) (Index, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Index{}, false, nil
		}
		return Index{}, false, nil
	}
	var file cacheFile
	if err := json.Unmarshal(data, &file); err != nil || file.SchemaVersion != SchemaVersion {
		return Index{}, false, nil
	}
	return file.Index, true, nil
}

// SaveCache writes the index. A failure is returned to the caller, which
// should keep the in-memory feed.
func SaveCache(dir string, idx Index) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return camperrors.Wrap(err, "creating dungeon feed cache")
	}
	data, err := json.Marshal(cacheFile{SchemaVersion: SchemaVersion, Index: idx})
	if err != nil {
		return camperrors.Wrap(err, "encoding dungeon feed cache")
	}
	path := filepath.Join(dir, "index.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return camperrors.Wrap(err, "writing dungeon feed cache")
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
	found, err := spelling.Discover(ctx, root)
	if err != nil {
		return Index{}, false, err
	}
	if fingerprintsMatch(ctx, root, cached, found) {
		return cached, false, nil
	}
	idx, err := Build(ctx, root)
	return idx, true, err
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
