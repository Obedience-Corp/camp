package workitem

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// hasWorkitemMarker reports whether the campaign-relative path holds a
// `.workitem` marker on disk. Backfill targets only paths with markers;
// intent .md files and festival directories have their own metadata and
// must not be flagged as "missing ref".
func hasWorkitemMarker(root, relPath string) bool {
	markerPath := filepath.Join(root, filepath.FromSlash(relPath), wkitem.MetadataFilename)
	info, err := os.Stat(markerPath)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func itemSourceRef(item wkitem.WorkItem) string {
	ref, _ := item.SourceMetadata["ref"].(string)
	return ref
}

// workitemPathsMissingRef returns campaign-relative paths from items whose
// `.workitem` marker exists and has no ref field. Callers must pass the
// already-discovered campaign items; this function does not walk the tree.
// Paths are sorted lexicographically so DeriveUnique's collision retry has
// deterministic input ordering during a doctor --fix pass.
func workitemPathsMissingRef(root string, items []wkitem.WorkItem) []string {
	var missing []string
	for _, item := range items {
		if !hasWorkitemMarker(root, item.RelativePath) {
			continue
		}
		if itemSourceRef(item) != "" {
			continue
		}
		missing = append(missing, item.RelativePath)
	}
	sort.Strings(missing)
	return missing
}

type backfillFailure struct {
	RelativePath string
	Err          error
}

// backfillMissingRefs writes unique refs onto items that still lack one.
// items must be the already-discovered campaign snapshot from the same doctor
// pass; this function does not Discover again.
func backfillMissingRefs(ctx context.Context, root string, items []wkitem.WorkItem) (int, []backfillFailure, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}

	existingRefs := make(map[string]bool, len(items))
	var pending []wkitem.WorkItem
	for _, item := range items {
		if !hasWorkitemMarker(root, item.RelativePath) {
			continue
		}
		ref := itemSourceRef(item)
		if ref != "" {
			existingRefs[ref] = true
			continue
		}
		pending = append(pending, item)
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].RelativePath < pending[j].RelativePath
	})

	applied := 0
	var failures []backfillFailure
	for _, item := range pending {
		if err := ctx.Err(); err != nil {
			return applied, failures, err
		}
		ref, err := wkitem.DeriveUnique(ctx, item.StableID, existingRefs)
		if err != nil {
			failures = append(failures, backfillFailure{RelativePath: item.RelativePath, Err: err})
			continue
		}
		if err := wkitem.BackfillRef(ctx, root, item.RelativePath, ref); err != nil {
			failures = append(failures, backfillFailure{RelativePath: item.RelativePath, Err: err})
			continue
		}
		existingRefs[ref] = true
		applied++
	}
	return applied, failures, nil
}
