package triage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// DiscoveryIndex is one discovery walk, indexed the ways refresh reads it.
//
// Building both indexes in a single pass is what keeps the cost at "one walk"
// rather than one per anchor kind: a workitem anchor and a festival anchor
// both resolve from this, with no filesystem access of their own.
type DiscoveryIndex struct {
	// ByStableID holds every item discoverable outside dungeons, keyed by the
	// same identity the manifest rows use.
	ByStableID map[string]DiscoveredItem
	// FestivalStatus maps a festival id to its observed status. Festivals are
	// discovered with the status as their lifecycle stage and the festival id
	// as their source id, so a festival anchor resolves without a second walk.
	FestivalStatus map[string]string
}

// IndexDiscovery builds the refresh index from a discovery walk.
//
// Items inside dungeons are excluded, using workitem.InDungeonPath so
// "outside dungeons" means the same thing here as it does to the sweep.
// Discover() already excludes dungeon subtrees; the filter is kept because
// "gone" is defined in terms of it and a defensive guard is cheaper than a
// wrong terminal verdict.
func IndexDiscovery(items []workitem.WorkItem) DiscoveryIndex {
	index := DiscoveryIndex{
		ByStableID:     make(map[string]DiscoveredItem, len(items)),
		FestivalStatus: make(map[string]string),
	}
	for _, item := range items {
		if workitem.InDungeonPath(item.RelativePath) {
			continue
		}
		id := StableIDFor(item)
		index.ByStableID[id] = DiscoveredItem{
			StableID:       id,
			Key:            item.Key,
			Type:           string(item.WorkflowType),
			Title:          item.Title,
			RelativePath:   item.RelativePath,
			LifecycleStage: lifecycleOrNone(item),
			AttentionStage: item.AttentionStage,
		}
		if item.WorkflowType == workitem.WorkflowTypeFestival && item.SourceID != "" {
			index.FestivalStatus[item.SourceID] = string(item.LifecycleStage)
		}
	}
	return index
}

// lifecycleOrNone mirrors the manifest's normalization so a row and its
// re-discovery compare equal on an item that simply has no lifecycle stage.
// Without this every such row would classify as moved on every refresh.
func lifecycleOrNone(item workitem.WorkItem) string {
	if item.LifecycleStage == "" {
		return string(workitem.LifecycleStageNone)
	}
	return string(item.LifecycleStage)
}

// CheckLocalAnchors re-checks every anchor that can be resolved without the
// network, returning results keyed by stable id in the input's own order.
//
// Cost is one hash per distinct path regardless of how many rows anchor it,
// and zero filesystem access for workitem and festival anchors — they read the
// discovery index the diff already built. Remote kinds (pr) are recorded
// unchecked here; 02_remote_anchors_and_invalidation gives them a real check.
func CheckLocalAnchors(
	ctx context.Context,
	campaignRoot string,
	byRow map[string][]Anchor,
	index DiscoveryIndex,
) (map[string][]AnchorCheck, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	hashes := newPathHasher(campaignRoot)
	out := make(map[string][]AnchorCheck, len(byRow))

	// Sorted so a cancellation mid-walk stops at a reproducible point and the
	// hash pass visits paths in a stable order.
	for _, stableID := range sortedKeys(byRow) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		anchors := byRow[stableID]
		checks := make([]AnchorCheck, 0, len(anchors))
		for _, anchor := range anchors {
			check, err := checkAnchor(anchor, hashes, index)
			if err != nil {
				return nil, err
			}
			checks = append(checks, check)
		}
		if len(checks) > 0 {
			out[stableID] = checks
		}
	}
	return out, nil
}

// checkAnchor observes one anchor's current value.
func checkAnchor(anchor Anchor, hashes *pathHasher, index DiscoveryIndex) (AnchorCheck, error) {
	switch anchor.Kind {
	case AnchorKindPath:
		hash, err := hashes.hash(anchor.Path)
		if err != nil {
			return AnchorCheck{}, err
		}
		return AnchorCheck{Anchor: anchor, Observed: hash}, nil

	case AnchorKindWorkitem:
		// A missing item observes the empty stage, which mismatches whatever
		// was recorded and lands the row in `changed`. That is the right
		// answer: the fact the verdict rested on is no longer observable.
		return AnchorCheck{
			Anchor:   anchor,
			Observed: index.ByStableID[anchor.StableID].AttentionStage,
		}, nil

	case AnchorKindFestival:
		return AnchorCheck{
			Anchor:   anchor,
			Observed: index.FestivalStatus[anchor.ID],
		}, nil

	default:
		// pr and any future remote kind. Recorded as an absence of evidence
		// rather than guessed at in either direction.
		return AnchorCheck{
			Anchor:    anchor,
			Observed:  ObservedUncheckedOffline,
			Unchecked: true,
		}, nil
	}
}

// pathHasher hashes campaign-relative paths, once each.
type pathHasher struct {
	root  string
	cache map[string]string
}

func newPathHasher(root string) *pathHasher {
	return &pathHasher{root: root, cache: make(map[string]string)}
}

// hash returns the sha256 of the file at relPath, in the anchor's recorded
// format. A path that does not exist hashes to the empty string, which
// mismatches any recorded hash and correctly classifies the row as changed:
// a deleted file is the strongest possible change to the fact it anchored.
func (h *pathHasher) hash(relPath string) (string, error) {
	if cached, ok := h.cache[relPath]; ok {
		return cached, nil
	}
	sum, err := hashFile(filepath.Join(h.root, relPath))
	if err != nil {
		return "", err
	}
	h.cache[relPath] = sum
	return sum, nil
}

// hashFile returns "sha256:<hex>" for a file's contents, or "" when it is
// absent. Streamed rather than read whole so a large anchored artifact does
// not size the process.
func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", camperrors.Wrapf(err, "open anchor path %s", path)
	}
	defer func() { _ = file.Close() }()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", camperrors.Wrapf(err, "read anchor path %s", path)
	}
	return PathHashPrefix + hex.EncodeToString(digest.Sum(nil)), nil
}

// AnchorsByRow collects the anchors of every evidence record in a run, keyed
// by stable id, so the checker sees each row's anchors in one place.
func (s *Store) AnchorsByRow(ctx context.Context, runID string, stableIDs []string) (map[string][]Anchor, error) {
	out := make(map[string][]Anchor, len(stableIDs))
	for _, id := range stableIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := s.Evidence(ctx, runID, id)
		if err != nil {
			return nil, err
		}
		// A row with no evidence record simply has no anchors: it is judged
		// without a re-checkable basis, so nothing about it can expire.
		if record != nil && len(record.Anchors) > 0 {
			out[id] = record.Anchors
		}
	}
	return out, nil
}
