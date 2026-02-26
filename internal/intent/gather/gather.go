package gather

import (
	"context"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/index"
)

// Service provides gather operations for intents.
// It coordinates between the intent service (for CRUD) and the index (for discovery).
type Service struct {
	intentSvc  *intent.IntentService
	index      *index.Index
	intentsDir string
}

// NewService creates a new gather service.
func NewService(intentSvc *intent.IntentService, intentsDir string) *Service {
	return &Service{
		intentSvc:  intentSvc,
		index:      index.NewIndex(intentsDir),
		intentsDir: intentsDir,
	}
}

// BuildIndex builds or rebuilds the content index.
// Only indexes active working set (inbox, active, ready).
func (s *Service) BuildIndex(ctx context.Context) error {
	return s.index.Build(ctx, nil)
}

// IndexSize returns the number of indexed intents.
func (s *Service) IndexSize() int {
	return s.index.Size()
}

// FindByTag returns intents matching a frontmatter tag.
func (s *Service) FindByTag(ctx context.Context, tag string) ([]*intent.Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	ids := s.index.FindByTag(tag)
	return s.loadIntents(ctx, ids)
}

// FindByHashtag returns intents containing a hashtag in their content.
func (s *Service) FindByHashtag(ctx context.Context, hashtag string) ([]*intent.Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	ids := s.index.FindByHashtag(hashtag)
	return s.loadIntents(ctx, ids)
}

// SimilarResult contains an intent and its similarity score.
type SimilarResult struct {
	Intent *intent.Intent
	Score  float64
}

// FindSimilar returns intents similar to the given ID.
func (s *Service) FindSimilar(ctx context.Context, id string, minScore float64) ([]SimilarResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	similar := s.index.FindSimilar(id, minScore)
	results := make([]SimilarResult, 0, len(similar))

	for _, sim := range similar {
		i, err := s.intentSvc.Get(ctx, sim.ID)
		if err != nil {
			continue // Skip intents that can't be loaded
		}
		if i.Status.InDungeon() {
			continue // Skip done/killed intents — not eligible for gathering
		}
		results = append(results, SimilarResult{
			Intent: i,
			Score:  sim.Score,
		})
	}

	return results, nil
}

// GetAllTags returns all unique frontmatter tags.
func (s *Service) GetAllTags() []string {
	return s.index.GetAllTags()
}

// GetAllHashtags returns all unique content hashtags.
func (s *Service) GetAllHashtags() []string {
	return s.index.GetAllHashtags()
}

// TagCounts returns tags with their occurrence counts.
func (s *Service) TagCounts() map[string]int {
	return s.index.TagCounts()
}

// HashtagCounts returns hashtags with their occurrence counts.
func (s *Service) HashtagCounts() map[string]int {
	return s.index.HashtagCounts()
}

// GatherOptions configures a gather operation.
type GatherOptions struct {
	Title          string          // Required: title for the gathered intent
	Type           intent.Type     // Optional: override type resolution
	Concept        string          // Optional: override concept resolution
	Priority       intent.Priority // Optional: override priority resolution
	Horizon        intent.Horizon  // Optional: override horizon resolution
	ArchiveSources bool            // Whether to archive source intents (default: true)
}

// GatherResult contains the result of a gather operation.
type GatherResult struct {
	Gathered      *intent.Intent // The newly created gathered intent
	ArchivedPaths []string       // Paths of archived source intents
	SourceCount   int            // Number of source intents gathered
}

// Gather combines multiple intents into a single gathered intent.
// Source intents are archived (moved to dungeon/archived) unless ArchiveSources is false.
func (s *Service) Gather(ctx context.Context, ids []string, opts GatherOptions) (*GatherResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	if len(ids) < 2 {
		return nil, fmt.Errorf("need at least 2 intents to gather, got %d", len(ids))
	}

	if opts.Title == "" {
		return nil, fmt.Errorf("title is required for gathered intent")
	}

	// Load source intents
	sources := make([]*intent.Intent, 0, len(ids))
	for _, id := range ids {
		i, err := s.intentSvc.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("loading intent %s: %w", id, err)
		}
		sources = append(sources, i)
	}

	// Merge intents
	mergeOpts := intent.MergeOptions{
		Title:    opts.Title,
		Type:     opts.Type,
		Concept:  opts.Concept,
		Priority: opts.Priority,
		Horizon:  opts.Horizon,
	}

	merged, err := intent.MergeIntents(sources, mergeOpts)
	if err != nil {
		return nil, fmt.Errorf("merging intents: %w", err)
	}

	// Save the gathered intent
	createOpts := intent.CreateOptions{
		Title:   merged.Title,
		Type:    merged.Type,
		Concept: merged.Concept,
		Body:    merged.Content,
	}

	// Create the gathered intent file
	gathered, err := s.intentSvc.CreateDirect(ctx, createOpts)
	if err != nil {
		return nil, fmt.Errorf("creating gathered intent: %w", err)
	}

	// Copy over the gathered metadata
	gathered.Priority = merged.Priority
	gathered.Horizon = merged.Horizon
	gathered.Tags = merged.Tags
	gathered.BlockedBy = merged.BlockedBy
	gathered.DependsOn = merged.DependsOn
	gathered.GatheredFrom = merged.GatheredFrom
	gathered.GatheredAt = merged.GatheredAt

	// Save with updated metadata
	if err := s.intentSvc.Save(ctx, gathered); err != nil {
		return nil, fmt.Errorf("saving gathered intent: %w", err)
	}

	result := &GatherResult{
		Gathered:    gathered,
		SourceCount: len(sources),
	}

	// Archive source intents
	if opts.ArchiveSources {
		for _, src := range sources {
			// Update source with gathered_into reference
			src.GatheredInto = gathered.ID
			if err := s.intentSvc.Save(ctx, src); err != nil {
				// Log but continue - non-fatal error
				continue
			}

			// Move to archived status
			archived, err := s.intentSvc.Move(ctx, src.ID, intent.StatusArchived)
			if err != nil {
				// Log but continue - non-fatal error
				continue
			}
			result.ArchivedPaths = append(result.ArchivedPaths, archived.Path)
		}
	}

	return result, nil
}

// loadIntents loads full intent objects from a list of IDs.
// Intents in final states (done/killed) are excluded.
func (s *Service) loadIntents(ctx context.Context, ids []string) ([]*intent.Intent, error) {
	intents := make([]*intent.Intent, 0, len(ids))
	for _, id := range ids {
		i, err := s.intentSvc.Get(ctx, id)
		if err != nil {
			continue // Skip intents that can't be loaded
		}
		if i.Status.InDungeon() {
			continue // Skip done/killed intents — not eligible for gathering
		}
		intents = append(intents, i)
	}
	return intents, nil
}
