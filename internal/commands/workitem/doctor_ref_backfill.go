package workitem

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
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

// workitemPathsMissingRef discovers every workitem on disk and returns the
// campaign-relative paths to those whose .workitem marker exists and has no
// ref field. Paths are sorted lexicographically so DeriveUnique's collision
// retry has deterministic input ordering during a doctor --fix pass.
func workitemPathsMissingRef(ctx context.Context, root string) ([]string, error) {
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return nil, camperrors.Wrap(err, "load campaign config")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, item := range items {
		if !hasWorkitemMarker(root, item.RelativePath) {
			continue
		}
		ref, _ := item.SourceMetadata["ref"].(string)
		if ref != "" {
			continue
		}
		missing = append(missing, item.RelativePath)
	}
	sort.Strings(missing)
	return missing, nil
}

type backfillFailure struct {
	RelativePath string
	Err          error
}

func backfillMissingRefs(ctx context.Context, root string) (int, []backfillFailure, error) {
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return 0, nil, camperrors.Wrap(err, "load campaign config")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return 0, nil, err
	}

	existingRefs := make(map[string]bool, len(items))
	var pending []wkitem.WorkItem
	for _, item := range items {
		if !hasWorkitemMarker(root, item.RelativePath) {
			continue
		}
		ref, _ := item.SourceMetadata["ref"].(string)
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
