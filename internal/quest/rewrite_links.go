package quest

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// PathMove is one filesystem relocation that may stale quest Link.Path values.
// Src and Dst may be absolute or campaign-root-relative.
type PathMove struct {
	Src string
	Dst string
}

type relMove struct {
	src string
	dst string
}

// RewriteLinksForMoves updates Link.Path on every quest (including dungeon
// quests) that pointed at a moved path. It returns campaign-root-relative
// slash-separated quest.yaml paths it modified, so callers can stage them
// with the move. Missing quest scaffolding is a no-op.
func RewriteLinksForMoves(ctx context.Context, campaignRoot string, moves []PathMove) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}
	if len(moves) == 0 || !Exists(campaignRoot) {
		return nil, nil
	}

	relMoves, err := normalizePathMoves(campaignRoot, moves)
	if err != nil {
		return nil, err
	}
	if len(relMoves) == 0 {
		return nil, nil
	}

	quests, err := List(ctx, campaignRoot, true)
	if err != nil {
		if errors.Is(err, ErrNotInitialized) {
			return nil, nil
		}
		return nil, camperrors.Wrap(err, "listing quests to rewrite links")
	}

	modified := make([]string, 0)
	for _, q := range quests {
		if err := ctx.Err(); err != nil {
			return nil, camperrors.Wrap(err, "context cancelled")
		}
		if !rewriteQuestLinkPaths(q, relMoves) {
			continue
		}
		q.UpdatedAt = nowUTC()
		if err := Save(ctx, q.Path, q); err != nil {
			return nil, camperrors.Wrapf(err, "saving rewritten quest links in %s", q.Path)
		}
		rel, relErr := filepath.Rel(campaignRoot, q.Path)
		if relErr != nil {
			return nil, camperrors.Wrapf(relErr, "relativizing rewritten quest path %s", q.Path)
		}
		modified = append(modified, filepath.ToSlash(rel))
	}
	sort.Strings(modified)
	return modified, nil
}

func normalizePathMoves(campaignRoot string, moves []PathMove) ([]relMove, error) {
	out := make([]relMove, 0, len(moves))
	for _, m := range moves {
		src, err := toCampaignRel(campaignRoot, m.Src)
		if err != nil {
			return nil, err
		}
		dst, err := toCampaignRel(campaignRoot, m.Dst)
		if err != nil {
			return nil, err
		}
		if src == "" || dst == "" || src == dst {
			continue
		}
		out = append(out, relMove{src: src, dst: dst})
	}
	return out, nil
}

func toCampaignRel(campaignRoot, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return cleanCampaignRelPath(path), nil
	}
	if campaignRoot == "" {
		return "", camperrors.Wrap(camperrors.ErrInvalidInput, "camp root is required to rewrite quest links")
	}
	rel, err := filepath.Rel(campaignRoot, cleaned)
	if err != nil {
		return "", camperrors.Wrapf(err, "relativizing move path %s", path)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "move path is the camp root: %s", path)
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "move path escapes camp root: %s", path)
	}
	return strings.TrimSuffix(rel, "/"), nil
}

// cleanCampaignRelPath cleans and slash-normalizes a campaign-relative path
// candidate: it collapses "./" prefixes, "//" runs, and trailing slashes to
// the same canonical form filepath.Clean produces, then converts to forward
// slashes for storage. Move src/dst (via toCampaignRel) and stored
// Link.Path values both run through this before comparison, because
// AddLink stores whatever path a caller passes (e.g. "./workflow/design/foo"
// from shell tab-completion) without normalizing it first, and a raw
// TrimSuffix-only comparison would silently leave that link stale.
//
// The empty string means "no path", matching toCampaignRel's contract that
// a Clean result of "." (the campaign root itself) is not a real relative
// path either.
func cleanCampaignRelPath(path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func rewriteQuestLinkPaths(q *Quest, moves []relMove) bool {
	if q == nil || len(q.Links) == 0 {
		return false
	}
	changed := false
	for i := range q.Links {
		newPath, ok := rewriteCampaignRelPath(q.Links[i].Path, moves)
		if !ok {
			continue
		}
		q.Links[i].Path = newPath
		changed = true
	}
	return changed
}

// rewriteCampaignRelPath maps a campaign-relative slash path through moves in
// order so chained relocations resolve to the final destination.
func rewriteCampaignRelPath(path string, moves []relMove) (string, bool) {
	current := cleanCampaignRelPath(path)
	if current == "" {
		return path, false
	}
	changed := false
	for _, m := range moves {
		if current != m.src && !strings.HasPrefix(current, m.src+"/") {
			continue
		}
		current = m.dst + strings.TrimPrefix(current, m.src)
		changed = true
	}
	if !changed {
		return path, false
	}
	return current, true
}
