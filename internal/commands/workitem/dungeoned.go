package workitem

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// dungeonedWorkitems maps the id of every shelved workitem to its
// campaign-relative dungeon path.
//
// wkitem.Discover deliberately skips dungeon directories, which is right for
// listing: completed work should not clutter `camp workitem list`. It is wrong
// as an existence test. A workitem promoted to completed still exists, still
// owns its id, and the links pointing at it still record something true --
// but to Discover it looks deleted, so doctor called those links broken and
// auto-fix removed them.
//
// This walk answers the narrower question doctor actually needs: is the
// workitem shelved rather than gone? It runs only in doctor, never on a hot
// path.
func dungeonedWorkitems(ctx context.Context, root string) map[string]string {
	out := map[string]string{}
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return out
	}
	resolver := paths.NewResolverFromConfig(root, cfg)

	_ = filepath.WalkDir(resolver.Workflow(), func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // an unreadable subtree is not a doctor failure
		}
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		if d.IsDir() || d.Name() != wkitem.MetadataFilename || !underDungeon(path) {
			return nil
		}
		meta, err := wkitem.LoadMetadata(ctx, filepath.Dir(path))
		if err != nil || meta == nil || meta.ID == "" {
			return nil
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			return nil
		}
		out[meta.ID] = filepath.ToSlash(rel)
		return nil
	})
	return out
}

// underDungeon reports whether path has a dungeon directory segment. Both
// spellings are accepted: campaigns predating the hidden layout still use the
// visible one, and camp dungeon migrate converts them lazily.
func underDungeon(path string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if seg == ".dungeon" || seg == "dungeon" {
			return true
		}
	}
	return false
}
