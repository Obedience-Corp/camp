package workflow

import (
	"context"
)

// Crawl interactively reviews items across statuses.
// This is a placeholder implementation - full TUI will be implemented later.
func (s *Service) Crawl(ctx context.Context, opts CrawlOptions) (*CrawlResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Load schema if not already loaded
	if s.schema == nil {
		if err := s.LoadSchema(ctx); err != nil {
			return nil, err
		}
	}

	result := &CrawlResult{
		Transitions: make(map[string]int),
	}

	// Determine which statuses to crawl
	var statuses []string
	if opts.Status != "" {
		statuses = []string{opts.Status}
	} else {
		statuses = s.schema.AllDirectories()
	}

	// Count items in each status
	for _, status := range statuses {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		listResult, err := s.List(ctx, status, ListOptions{})
		if err != nil {
			continue
		}
		result.Reviewed += len(listResult.Items)
		result.Kept += len(listResult.Items) // For now, just mark as kept
	}

	return result, nil
}
