package index

import (
	"context"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/nav"
)

func resolveFestivalID(ctx context.Context, opts ResolveOptions) (*ResolveResult, error) {
	if opts.Category != "" && opts.Category != nav.CategoryAll && opts.Category != nav.CategoryFestivals {
		return nil, nil
	}
	path, err := nav.ResolveFestivalID(ctx, opts.CampaignRoot, opts.Query)
	if err != nil || path == "" {
		return nil, err
	}
	target := &Target{Name: filepath.Base(path), Path: path, Category: nav.CategoryFestivals}
	return &ResolveResult{Path: target.Path, Name: target.Name,
		Category: target.Category, Exact: true, Target: target}, nil
}
