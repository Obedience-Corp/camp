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
	if q.IsDefault() {
		return nil, ErrDefaultQuestReadOnly
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
