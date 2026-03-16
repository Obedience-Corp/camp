package intent

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/sahilm/fuzzy"
)

// ListOptions contains options for listing intents.
type ListOptions struct {
	Status   *Status // Filter by status (nil for all)
	Type     *Type   // Filter by type (nil for all)
	Concept  string  // Filter by concept (empty for all)
	SortBy   string  // Sort field: "created", "updated", "title", "priority"
	SortDesc bool    // Sort in descending order
}

// Find locates an intent by ID across all status directories.
// Supports fuzzy matching - partial IDs will match.
func (s *IntentService) Find(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	statuses := AllStatuses()

	// First try exact match
	for _, status := range statuses {
		path := s.getIntentPath(status, id)
		if intent, err := s.loadIntent(path); err == nil {
			return intent, nil
		}
	}

	// Try fuzzy match (ID contains the search term)
	for _, status := range statuses {
		dir := filepath.Join(s.intentsDir, string(status))
		files, err := os.ReadDir(dir)
		if err != nil {
			continue // Directory may not exist
		}

		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".md") {
				continue
			}
			baseName := strings.TrimSuffix(file.Name(), ".md")
			if strings.Contains(baseName, id) {
				path := filepath.Join(dir, file.Name())
				if intent, err := s.loadIntent(path); err == nil {
					return intent, nil
				}
			}
		}
	}

	return nil, camperrors.Wrap(ErrNotFound, id)
}

// Get retrieves an intent by its exact ID.
func (s *IntentService) Get(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	for _, status := range AllStatuses() {
		path := s.getIntentPath(status, id)
		if intent, err := s.loadIntent(path); err == nil {
			return intent, nil
		}
	}

	return nil, camperrors.Wrap(ErrNotFound, id)
}

// List returns all intents matching the given options.
func (s *IntentService) List(ctx context.Context, opts *ListOptions) ([]*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	var intents []*Intent
	statuses := AllStatuses()

	if opts != nil && opts.Status != nil {
		statuses = []Status{*opts.Status}
	}

	for _, status := range statuses {
		if err := ctx.Err(); err != nil {
			return nil, camperrors.Wrap(err, "context cancelled")
		}

		dir := filepath.Join(s.intentsDir, string(status))
		files, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, camperrors.Wrapf(err, "reading directory %s", dir)
		}

		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, file.Name())
			intent, err := s.loadIntent(path)
			if err != nil {
				continue
			}

			if opts != nil {
				if opts.Type != nil && intent.Type != *opts.Type {
					continue
				}
				if opts.Concept != "" && intent.Concept != opts.Concept {
					continue
				}
			}

			intents = append(intents, intent)
		}
	}

	seen := make(map[string]bool, len(intents))
	deduped := make([]*Intent, 0, len(intents))
	for _, i := range intents {
		if seen[i.ID] {
			continue
		}
		seen[i.ID] = true
		deduped = append(deduped, i)
	}
	intents = deduped

	if opts != nil && opts.SortBy != "" {
		s.sortIntents(intents, opts.SortBy, opts.SortDesc)
	} else {
		s.sortIntents(intents, "updated", true)
	}

	return intents, nil
}

// intentSource implements fuzzy.Source interface for intent searching.
type intentSource []*Intent

func (is intentSource) String(i int) string { return is[i].Title }

func (is intentSource) Len() int { return len(is) }

// Search returns intents matching the query string using fuzzy matching.
// Empty query returns all intents. Results are sorted by relevance score.
func (s *IntentService) Search(ctx context.Context, query string) ([]*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	allIntents, err := s.List(ctx, nil)
	if err != nil {
		return nil, camperrors.Wrap(err, "listing intents")
	}

	if query == "" {
		return allIntents, nil
	}

	matches := fuzzy.FindFrom(query, intentSource(allIntents))
	results := make([]*Intent, len(matches))
	for i, match := range matches {
		results[i] = allIntents[match.Index]
	}

	return results, nil
}

// sortIntents sorts a slice of intents by the given field.
func (s *IntentService) sortIntents(intents []*Intent, sortBy string, desc bool) {
	sort.Slice(intents, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "created":
			less = intents[i].CreatedAt.Before(intents[j].CreatedAt)
		case "updated":
			ui := intents[i].UpdatedAt
			if ui.IsZero() {
				ui = intents[i].CreatedAt
			}
			uj := intents[j].UpdatedAt
			if uj.IsZero() {
				uj = intents[j].CreatedAt
			}
			less = ui.Before(uj)
		case "title":
			less = intents[i].Title < intents[j].Title
		case "priority":
			less = priorityRank(intents[i].Priority) < priorityRank(intents[j].Priority)
		default:
			less = intents[i].CreatedAt.Before(intents[j].CreatedAt)
		}
		if desc {
			return !less
		}
		return less
	})
}
