package quest

import (
	"context"
	"errors"
	"strings"
	"time"
)

type MetadataUpdateOptions struct {
	Purpose     *string
	Description *string
}

// WorkitemEnrichment carries the metadata extracted from a linked workitem
// (or festival) that can populate a quest whose own fields are still
// placeholder-empty. The command layer resolves the workitem and passes its
// Title and Summary as plain strings, so internal/quest stays free of
// workitem-package coupling.
type WorkitemEnrichment struct {
	Title   string
	Summary string
}

// EnrichFromWorkitem populates empty quest Purpose and Description fields from
// a linked workitem's metadata. Non-empty fields are never overwritten — this
// is the invariant camp#583 requires: "never overwrite metadata the user
// actually supplied."
//
// When both fields are already set the call is a no-op. When at least one field
// is enriched the quest is re-saved and returned in a MutationResult; otherwise
// the original quest is returned with an empty Files slice (no write, no
// commit).
func (s *Service) EnrichFromWorkitem(ctx context.Context, identifier string, enrichment WorkitemEnrichment) (*MutationResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	q, err := Resolve(ctx, s.campaignRoot, identifier)
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(enrichment.Title)
	summary := strings.TrimSpace(enrichment.Summary)

	changed := false
	if q.Purpose == "" && title != "" {
		q.Purpose = title
		changed = true
	}
	if q.Description == "" && summary != "" {
		q.Description = summary
		changed = true
	}

	if !changed {
		return &MutationResult{Quest: q}, nil
	}

	q.UpdatedAt = time.Now().UTC()
	if err := Save(ctx, q.Path, q); err != nil {
		return nil, err
	}

	return &MutationResult{
		Quest: q,
		Files: []string{q.Path},
	}, nil
}

func (s *Service) UpdateMetadata(ctx context.Context, identifier string, opts MetadataUpdateOptions) (*MutationResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}
	if opts.Purpose == nil && opts.Description == nil {
		return nil, errors.New("at least one quest metadata field is required")
	}

	q, err := Resolve(ctx, s.campaignRoot, identifier)
	if err != nil {
		return nil, err
	}

	if opts.Purpose != nil {
		q.Purpose = strings.TrimSpace(*opts.Purpose)
	}
	if opts.Description != nil {
		q.Description = strings.TrimSpace(*opts.Description)
	}
	q.UpdatedAt = time.Now().UTC()
	if err := Save(ctx, q.Path, q); err != nil {
		return nil, err
	}

	return &MutationResult{
		Quest: q,
		Files: []string{q.Path},
	}, nil
}
