// Package moveref rewrites campaign references after a file or directory move.
// Markdown links and quest Link.Path values share this surface so promote,
// rename, gather, and dungeon moves cannot drift.
package moveref

import (
	"context"
	"sort"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/mdlinks"
	"github.com/Obedience-Corp/camp/internal/quest"
)

// RewriteForMove updates relative markdown links and quest Link.Path values
// after a single file or directory move. It returns campaign-root-relative
// paths of every file it modified so callers can stage them with the move.
func RewriteForMove(ctx context.Context, campaignRoot, srcPath, dstPath string) ([]string, error) {
	internal, err := mdlinks.RewriteMovedInternalLinks(ctx, campaignRoot, srcPath, dstPath)
	if err != nil {
		return nil, err
	}
	external, err := RewriteExternal(ctx, campaignRoot, []mdlinks.Move{{Src: srcPath, Dst: dstPath}})
	if err != nil {
		return nil, err
	}
	return mergeRelPaths(internal, external), nil
}

// RewriteMoves rewrites internal markdown in each moved subtree, then external
// markdown and quest links for the whole batch in one pass.
func RewriteMoves(ctx context.Context, campaignRoot string, moves []mdlinks.Move) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}
	var internal []string
	for _, m := range moves {
		files, err := mdlinks.RewriteMovedInternalLinks(ctx, campaignRoot, m.Src, m.Dst)
		if err != nil {
			return nil, err
		}
		internal = mergeRelPaths(internal, files)
	}
	external, err := RewriteExternal(ctx, campaignRoot, moves)
	if err != nil {
		return nil, err
	}
	return mergeRelPaths(internal, external), nil
}

// RewriteExternal rewrites campaign markdown files and quest.yaml Link.Path
// values that pointed at any of the moved items. Call this when internal
// markdown rewrites already ran (dungeon batch mode).
func RewriteExternal(ctx context.Context, campaignRoot string, moves []mdlinks.Move) ([]string, error) {
	md, err := mdlinks.RewriteExternalLinksForMoves(ctx, campaignRoot, moves)
	if err != nil {
		return nil, err
	}
	qMoves := make([]quest.PathMove, len(moves))
	for i, m := range moves {
		qMoves[i] = quest.PathMove{Src: m.Src, Dst: m.Dst}
	}
	q, err := quest.RewriteLinksForMoves(ctx, campaignRoot, qMoves)
	if err != nil {
		return nil, err
	}
	return mergeRelPaths(md, q), nil
}

func mergeRelPaths(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, p := range append(append([]string{}, a...), b...) {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
